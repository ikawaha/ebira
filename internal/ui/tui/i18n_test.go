package tui

import (
	"context"
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestMessages checks that every language has the keys of English, with the
// same {name} placeholders.
func TestMessages(t *testing.T) {
	read := func(lang string) map[string]string {
		t.Helper()
		b, err := i18nFS.ReadFile("i18n/" + lang + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]string
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		return m
	}
	placeholder := regexp.MustCompile(`\{[a-z]+\}`)
	names := func(s string) []string {
		ns := placeholder.FindAllString(s, -1)
		slices.Sort(ns)
		return slices.Compact(ns)
	}
	en := read("en")
	for _, lang := range Langs[1:] {
		m := read(lang)
		for _, k := range slices.Sorted(maps.Keys(en)) {
			v, ok := m[k]
			if !ok {
				t.Errorf("%s: missing %s", lang, k)
				continue
			}
			if !slices.Equal(names(v), names(en[k])) {
				t.Errorf("%s: %s has %v, want %v", lang, k, names(v), names(en[k]))
			}
		}
		for k := range m {
			if _, ok := en[k]; !ok {
				t.Errorf("%s: %s is not in en", lang, k)
			}
		}
	}
}

func TestCatalog(t *testing.T) {
	ja := newCatalog("ja")
	if got := ja.t("status.saved", "title", "計画"); got != "「計画」を保存しました" {
		t.Errorf("ja: %q", got)
	}
	if got := newCatalog("fr").t("status.saved", "title", "Plan"); got != `saved "Plan"` {
		t.Errorf("unknown language: %q", got)
	}
	if got := ja.t("no.such.key"); got != "no.such.key" {
		t.Errorf("missing key: %q", got)
	}
	err := errMsg("draft.badID", "id", "X")
	if !strings.HasPrefix(err.Error(), `invalid id "X"`) {
		t.Errorf("Error: %q", err.Error())
	}
	m := New(context.Background(), &Clients{}, false, "ja")
	if got := m.errText(err); !strings.HasPrefix(got, "id「X」は使えません") {
		t.Errorf("errText: %q", got)
	}
}

func TestLang(t *testing.T) {
	t.Setenv("LANG", "ja_JP.UTF-8")
	if got := Lang(""); got != "ja" {
		t.Errorf("$LANG ja: %s", got)
	}
	if got := Lang("en"); got != "en" {
		t.Errorf("given en: %s", got)
	}
	t.Setenv("LANG", "C")
	if got := Lang(""); got != "en" {
		t.Errorf("$LANG C: %s", got)
	}
}

// TestJapaneseView renders the TUI in Japanese, so that no English message is
// left in the panes and the footer.
func TestJapaneseView(t *testing.T) {
	m := New(context.Background(), &Clients{}, false, "ja")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 20})
	m = tm.(Model)
	view := ansi.Strip(m.render())
	for _, want := range []string{"ノートブック", "ノート · 手動", "ドキュメント", "ノートがありません。n で作れます。", "ノートを選んでいません。", "○ 切断", "移動", "終了"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q in\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"Notebooks", "Preview", "No notes", "quit"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("%q in\n%s", unwanted, view)
		}
	}
}
