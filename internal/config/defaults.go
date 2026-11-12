package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/ikawaha/ebira/internal/store"
)

// The settings of each machine have defaults shared with the notes: the
// mapping defaults of _ebira.yaml in the notes directory (store.ConfigFile),
// with the keys of the settings file. A value of the settings file wins over
// its default, so a machine can differ from the others. Keys that belong to
// a machine alone are left out of the defaults with a warning: notes_dir,
// which tells where the notes are, auth, as passkeys are kept by each
// machine and the notes should not turn them off, and an addr that is not of
// the loopback, so that the notes do not open the server to the network.

// DefaultKeys are the keys of the defaults that the web UI changes: those of
// UpdatedKeys other than auth.
var DefaultKeys = []string{"lang", "theme", "git_sync", "watch", "provider", "model", "tui_preview"}

// LoadDefaults reads the defaults of the settings file in _ebira.yaml of the
// notes directory notesDir. A missing file has none. It also returns a
// warning for each key left out, as belonging to a machine alone.
func LoadDefaults(notesDir string) (Config, []string, error) {
	p := filepath.Join(notesDir, store.ConfigFile)
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil, nil
	}
	if err != nil {
		return Config{}, nil, err
	}
	// The other keys of the file are read by the server (store.Config).
	var f struct {
		Defaults yaml.Node `yaml:"defaults"`
	}
	if err := yaml.Unmarshal(b, &f); err != nil {
		return Config{}, nil, fmt.Errorf("%s: %w", p, err)
	}
	return ParseDefaults(&f.Defaults, p)
}

// ParseDefaults reads the defaults node d of _ebira.yaml at path p, as
// LoadDefaults does. An unknown key is an error, as in the settings file.
func ParseDefaults(d *yaml.Node, p string) (Config, []string, error) {
	var c Config
	if d.Kind == 0 {
		return c, nil, nil
	}
	if d.Kind != yaml.MappingNode {
		return c, nil, fmt.Errorf("%s: defaults: not a mapping of the keys of the settings file", p)
	}
	// Decoding the node itself cannot refuse unknown keys.
	b, err := yaml.Marshal(d)
	if err != nil {
		return c, nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, nil, fmt.Errorf("%s: defaults: %w", p, plainError(err))
	}
	var warnings []string
	drop := func(key string) {
		warnings = append(warnings, fmt.Sprintf("%s: defaults: %s is left out: write it in the settings file of each machine", p, key))
	}
	if c.NotesDir != "" {
		drop("notes_dir")
		c.NotesDir = ""
	}
	if c.LegacyDataDir != "" {
		drop("data_dir")
		c.LegacyDataDir = ""
	}
	if c.Auth != "" {
		drop("auth")
		c.Auth = ""
	}
	if c.Addr != "" && !Loopback(c.Addr) {
		drop("addr " + c.Addr + ", not of the loopback,")
		c.Addr = ""
	}
	return c, warnings, nil
}

// Loopback reports whether the address addr, host:port, listens only on the
// loopback: localhost or a loopback IP address. An empty host listens on all
// addresses.
func Loopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Merge returns c with each value it does not set taken from d, its
// defaults.
func Merge(c, d Config) Config {
	or := func(v *string, def string) {
		if *v == "" {
			*v = def
		}
	}
	or(&c.NotesDir, d.NotesDir)
	or(&c.Addr, d.Addr)
	or(&c.Provider, d.Provider)
	or(&c.Model, d.Model)
	or(&c.Lang, d.Lang)
	or(&c.Theme, d.Theme)
	or(&c.Auth, d.Auth)
	or(&c.GitSync, d.GitSync)
	or(&c.TUIPreview, d.TUIPreview)
	if c.Watch == nil {
		c.Watch = d.Watch
	}
	return c
}
