package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"github.com/ikawaha/ebira/internal/authclient"
	"github.com/ikawaha/ebira/internal/claudecode"
)

// claudeState is the answer of GET /claude/mcp: whether this server can
// register itself with Claude Code, and what Claude Code has.
type claudeState struct {
	// Available is true when the request comes from this machine and claude
	// is found; the web UI shows its button only then.
	Available bool `json:"available"`
	// Reason tells why it is not available: remote (the request comes from
	// another machine, whose Claude Code this server cannot reach), not_found
	// or failed.
	Reason string `json:"reason,omitempty"`
	// Error is the output of claude when it failed.
	Error   string `json:"error,omitempty"`
	URL     string `json:"url"`
	Command string `json:"command"`
	// Registered is the URL of ebira that Claude Code has in the user scope.
	Registered string `json:"registered,omitempty"`
	// Skill is the state of the skill that opens whiteboards
	// (claudecode.SkillStatus): missing, current, outdated or modified.
	Skill string `json:"skill,omitempty"`
	// SkillPath is where the skill is.
	SkillPath string `json:"skill_path,omitempty"`
}

// claudeAdded is the answer of POST /claude/mcp.
type claudeAdded struct {
	claudecode.Result
	// Skill is the state of the skill that opens whiteboards before
	// POST /claude/mcp installed it: missing, outdated, current (nothing to
	// do) or modified (left as it is).
	Skill string `json:"skill"`
}

// claudeHandler serves GET /claude/mcp, and POST /claude/mcp, which registers
// this server with Claude Code in the user scope (claudecode.Add), and
// installs the skill that opens whiteboards (claudecode.InstallSkill). Both run
// claude of this machine, so they serve requests from this machine only: a
// browser on another machine would register ebira with a Claude Code it
// does not use. The request carries nothing to register: the URL comes from
// addr, so it cannot register other servers. With passkeys (check is not
// nil), it asks for a session signed in with a passkey, as the REST API does.
func claudeHandler(addr string, find func(context.Context) (claudecode.Client, error), check func(session string) (valid, verified bool)) http.Handler {
	url := claudecode.URL(addr)
	var mu sync.Mutex // one run of claude at a time
	reply := func(w http.ResponseWriter, code int, v any) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	fail := func(w http.ResponseWriter, code int, name, message string) {
		reply(w, code, map[string]string{"name": name, "message": message})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if check != nil {
			c, err := r.Cookie(authclient.CookieName)
			if err != nil || !first(check(c.Value)) {
				fail(w, http.StatusUnauthorized, "unauthorized", "sign in with a passkey")
				return
			}
		}
		st := claudeState{URL: url, Command: claudecode.Command(url)}
		if !fromLoopback(r) {
			if r.Method == http.MethodPost {
				fail(w, http.StatusForbidden, "forbidden", "register ebira with Claude Code on the machine of the server")
				return
			}
			st.Reason = "remote"
			reply(w, http.StatusOK, st)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		ctx := r.Context()
		c, err := find(ctx)
		if err != nil {
			if r.Method == http.MethodPost {
				fail(w, http.StatusConflict, "not_found", err.Error())
				return
			}
			st.Reason = "not_found"
			reply(w, http.StatusOK, st)
			return
		}
		configDir, err := claudecode.ConfigDir(ctx)
		if err != nil {
			fail(w, http.StatusInternalServerError, "failed", err.Error())
			return
		}
		if r.Method == http.MethodPost {
			res, err := c.Add(ctx, url)
			if err != nil {
				slog.ErrorContext(ctx, "claude mcp add failed", "error", err)
				fail(w, http.StatusBadGateway, "failed", err.Error())
				return
			}
			slog.InfoContext(ctx, "registered with Claude Code", "url", url, "status", res.Status)
			skill, err := claudecode.InstallSkill(configDir, claudecode.SkillName)
			if err != nil && !errors.Is(err, claudecode.ErrSkillModified) {
				slog.ErrorContext(ctx, "installing the skill failed", "error", err)
				fail(w, http.StatusInternalServerError, "failed", err.Error())
				return
			}
			slog.InfoContext(ctx, "installed the skill of Claude Code", "skill", claudecode.SkillName, "found", skill)
			reply(w, http.StatusOK, claudeAdded{Result: res, Skill: skill})
			return
		}
		reg, err := c.Get(ctx)
		if err != nil {
			st.Reason, st.Error = "failed", err.Error()
			reply(w, http.StatusOK, st)
			return
		}
		st.Available = true
		if reg != nil {
			st.Registered = reg.URL
		}
		st.SkillPath = claudecode.SkillPath(configDir, claudecode.SkillName)
		if st.Skill, err = claudecode.SkillStatus(configDir, claudecode.SkillName); err != nil {
			st.Skill = ""
		}
		reply(w, http.StatusOK, st)
	})
}

// fromLoopback reports whether the request comes from this machine: from a
// loopback address, to localhost or a loopback address. A proxy on this
// machine, such as tailscale serve, passes on requests of other machines from
// a loopback address, with the name it serves in Host.
func fromLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !loopback(host) {
		return false
	}
	name := r.Host
	if h, _, err := net.SplitHostPort(r.Host); err == nil {
		name = h
	}
	return loopback(name)
}
