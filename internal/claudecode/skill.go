package claudecode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// The skills ebira gives Claude Code. A skill is a directory of the skills
// directory of the configuration of Claude Code, with SKILL.md in it.
//
//go:embed skills
var skills embed.FS

// SkillName is the skill that opens whiteboards: it tells Claude Code how to
// send a text to start_ai_board without writing it out, with the ID of its
// session, and to read what the user gives back with get_ai_board.
const SkillName = "ebira-whiteboard"

// Skill states, as SkillStatus tells them.
const (
	// SkillMissing: there is no SKILL.md.
	SkillMissing = "missing"
	// SkillCurrent: ebira wrote the SKILL.md of this version.
	SkillCurrent = "current"
	// SkillOutdated: ebira wrote it, from another version, and nobody
	// changed it since; installing replaces it.
	SkillOutdated = "outdated"
	// SkillModified: the user wrote or changed it; installing leaves it.
	SkillModified = "modified"
)

// ErrSkillModified is the error of InstallSkill when the user wrote or
// changed the skill.
var ErrSkillModified = errors.New("the skill was changed by hand")

// skillMark is the line ebira puts after the front matter of a SKILL.md it
// writes, with the hash of the rest of the file: it tells a file ebira wrote,
// and unchanged, from one changed by hand.
var skillMark = regexp.MustCompile(`(?m)^<!-- written by ebira: ([0-9a-f]{16}); ebira mcp add replaces it -->\n`)

// skillHash is the hash that the mark of content carries.
func skillHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:8])
}

// skillFile returns the SKILL.md ebira writes for name: the embedded one,
// with the mark after its front matter.
func skillFile(name string) ([]byte, error) {
	src, err := skills.ReadFile("skills/" + name + "/SKILL.md")
	if err != nil {
		return nil, err
	}
	mark := fmt.Sprintf("<!-- written by ebira: %s; ebira mcp add replaces it -->\n", skillHash(src))
	// The front matter is between the first two lines of ---.
	if !bytes.HasPrefix(src, []byte("---\n")) {
		return nil, fmt.Errorf("skill %s has no front matter", name)
	}
	end := bytes.Index(src[4:], []byte("\n---\n"))
	if end < 0 {
		return nil, fmt.Errorf("skill %s has no front matter", name)
	}
	at := 4 + end + len("\n---\n")
	out := append([]byte{}, src[:at]...)
	out = append(out, mark...)
	return append(out, src[at:]...), nil
}

// SkillPath returns the SKILL.md of name in the configuration directory of
// Claude Code.
func SkillPath(configDir, name string) string {
	return filepath.Join(configDir, "skills", name, "SKILL.md")
}

// SkillStatus tells the state of the skill name in configDir.
func SkillStatus(configDir, name string) (string, error) {
	want, err := skillFile(name)
	if err != nil {
		return "", err
	}
	have, err := os.ReadFile(SkillPath(configDir, name))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return SkillMissing, nil
	case err != nil:
		return "", err
	case bytes.Equal(have, want):
		return SkillCurrent, nil
	}
	m := skillMark.FindSubmatchIndex(have)
	if m == nil {
		return SkillModified, nil
	}
	rest := append(append([]byte{}, have[:m[0]]...), have[m[1]:]...)
	if skillHash(rest) != string(have[m[2]:m[3]]) {
		return SkillModified, nil
	}
	return SkillOutdated, nil
}

// InstallSkill writes the skill name into configDir, unless it is current,
// and returns the state it found. It leaves a skill the user wrote or
// changed, with ErrSkillModified.
func InstallSkill(configDir, name string) (string, error) {
	st, err := SkillStatus(configDir, name)
	if err != nil {
		return "", err
	}
	switch st {
	case SkillCurrent:
		return st, nil
	case SkillModified:
		return st, ErrSkillModified
	}
	data, err := skillFile(name)
	if err != nil {
		return "", err
	}
	path := SkillPath(configDir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".SKILL-*.md")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return "", err
	}
	return st, os.Rename(tmp.Name(), path)
}

// ConfigDir returns the configuration directory of Claude Code:
// CLAUDE_CONFIG_DIR, else that of the user's login shell, else ~/.claude. A
// server started by the desktop app, from the Finder, has no variables of the
// shell.
func ConfigDir(ctx context.Context) (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	if runtime.GOOS != "windows" {
		if dir := configDirFromShell(ctx); dir != "" {
			return dir, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// configDirFromShell returns CLAUDE_CONFIG_DIR of the user's login shell, or
// "" without one.
func configDirFromShell(ctx context.Context) string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, shell, "-lc", `printf '%s\n' "$CLAUDE_CONFIG_DIR"`).Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if dir := lines[len(lines)-1]; filepath.IsAbs(dir) {
		return dir
	}
	return ""
}
