package claudecode

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillFile(t *testing.T) {
	data, err := skillFile(SkillName)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	// Claude Code reads the front matter only at the top of the file.
	if !strings.HasPrefix(s, "---\nname: "+SkillName+"\ndescription: ") {
		t.Errorf("SKILL.md begins with:\n%s", s[:min(len(s), 200)])
	}
	if !skillMark.Match(data) {
		t.Error("SKILL.md has no mark")
	}
	for _, want := range []string{"start_ai_board", "get_ai_board", "${CLAUDE_SESSION_ID}", "curl -sS -T -"} {
		if !strings.Contains(s, want) {
			t.Errorf("SKILL.md does not tell %s", want)
		}
	}
}

func TestInstallSkill(t *testing.T) {
	dir := t.TempDir()
	path := SkillPath(dir, SkillName)
	install := func(want string, wantErr error) {
		t.Helper()
		got, err := InstallSkill(dir, SkillName)
		if got != want || !errors.Is(err, wantErr) {
			t.Fatalf("InstallSkill = %q, %v; want %q, %v", got, err, want, wantErr)
		}
	}
	install(SkillMissing, nil)
	want, _ := skillFile(SkillName)
	if have, err := os.ReadFile(path); err != nil || !bytes.Equal(have, want) {
		t.Fatalf("installed %q, %v", have, err)
	}
	install(SkillCurrent, nil)

	// A skill of another version of ebira is replaced.
	old := []byte("---\nname: ebira-whiteboard\ndescription: old\n---\nold steps\n")
	mark := "<!-- written by ebira: " + skillHash(old) + "; ebira mcp add replaces it -->\n"
	at := bytes.Index(old, []byte("old steps"))
	if err := os.WriteFile(path, append(append(append([]byte{}, old[:at]...), mark...), old[at:]...), 0o644); err != nil {
		t.Fatal(err)
	}
	if st, _ := SkillStatus(dir, SkillName); st != SkillOutdated {
		t.Errorf("status of an old skill = %q", st)
	}
	install(SkillOutdated, nil)
	if have, _ := os.ReadFile(path); !bytes.Equal(have, want) {
		t.Error("the old skill was not replaced")
	}

	// One changed by hand, with the mark or without it, is left.
	for _, changed := range [][]byte{
		bytes.Replace(want, []byte("end your turn"), []byte("wait"), 1),
		[]byte("---\nname: ebira-whiteboard\ndescription: mine\n---\nmy steps\n"),
	} {
		if err := os.WriteFile(path, changed, 0o644); err != nil {
			t.Fatal(err)
		}
		install(SkillModified, ErrSkillModified)
		if have, _ := os.ReadFile(path); !bytes.Equal(have, changed) {
			t.Error("a skill changed by hand was replaced")
		}
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("the skill directory has %d files", len(entries))
	}
}

func TestConfigDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/home/someone/.claude-work")
	if dir, err := ConfigDir(context.Background()); err != nil || dir != "/home/someone/.claude-work" {
		t.Errorf("ConfigDir = %q, %v", dir, err)
	}
}
