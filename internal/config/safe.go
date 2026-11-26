package config

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SafeEnv names the environment variable of the safe mode: set to anything
// but 0, the commands start even when the settings file cannot be read, as
// with an error of YAML, without it (Salvage). The desktop app sets it when
// asked to start the server in safe mode, so that the file can be fixed.
// A server started again by itself keeps its environment, and reads the file
// as usual once it is fixed.
const SafeEnv = "EBIRA_SAFE_MODE"

// Safe reports whether the safe mode is on.
func Safe() bool {
	v := os.Getenv(SafeEnv)
	return v != "" && v != "0"
}

// Salvage reads what the safe mode takes from a settings file that Load
// refuses: notes_dir, from its own line at the top level, when that line
// reads, so that the server serves the notes it always does. The rest is
// left at the defaults.
func Salvage() Config {
	var c Config
	p, err := Path()
	if err != nil {
		return c
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return c
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "notes_dir:") {
			continue
		}
		var v struct {
			NotesDir string `yaml:"notes_dir"`
		}
		if yaml.Unmarshal([]byte(line), &v) != nil || v.NotesDir == "" {
			continue
		}
		if dir, err := resolve(v.NotesDir, filepath.Dir(p)); err == nil {
			c.NotesDir = dir
		}
		break
	}
	return c
}
