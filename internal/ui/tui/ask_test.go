package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	genassistant "github.com/ikawaha/ebira/gen/assistant"
)

func press(t *testing.T, m Model, keys ...tea.KeyPressMsg) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		var tm tea.Model
		tm, cmd = m.Update(k)
		m = tm.(Model)
	}
	return m, cmd
}

func text(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Text: s, Code: []rune(s)[0]}
}

func TestAsk(t *testing.T) {
	m := New(context.Background(), &Clients{}, false, "en")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = tm.(Model)

	m, _ = press(t, m, text("?"))
	if m.prompt != promptAsk {
		t.Fatalf("? did not open the question prompt")
	}
	m, _ = press(t, m, text("天"), text("気"))
	m, cmd := press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || m.answer == nil || !m.answer.pending || m.answer.question != "天気" {
		t.Fatalf("enter did not ask: answer %+v", m.answer)
	}
	view := ansi.Strip(m.render())
	if !strings.Contains(view, "Asking…") || !strings.Contains(view, "Q. 天気") {
		t.Errorf("view while asking:\n%s", view)
	}

	// A stale answer is ignored.
	tm, _ = m.Update(answerMsg{seq: m.askSeq - 1, text: "old"})
	m = tm.(Model)
	if !m.answer.pending {
		t.Fatal("a stale answer was applied")
	}
	tm, _ = m.Update(answerMsg{seq: m.askSeq, text: "雨が降ります。**tokyo** を参照。"})
	m = tm.(Model)
	view = ansi.Strip(m.render())
	if !strings.Contains(view, "Answer") || !strings.Contains(view, "雨が降ります。") {
		t.Errorf("view with the answer:\n%s", view)
	}

	// esc goes back to the note.
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.answerOn || strings.Contains(ansi.Strip(m.render()), "雨が降ります。") {
		t.Error("esc did not close the answer")
	}
}

func TestAskError(t *testing.T) {
	m := New(context.Background(), &Clients{}, false, "en")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = tm.(Model)
	m, _ = press(t, m, text("?"), text("x"), tea.KeyPressMsg{Code: tea.KeyEnter})
	tm, _ = m.Update(answerMsg{seq: m.askSeq, err: genassistant.MakeUnavailable(errors.New("the assistant is not configured"))})
	m = tm.(Model)
	if view := ansi.Strip(m.render()); !strings.Contains(view, "the assistant is not configured") {
		t.Errorf("view with an error:\n%s", view)
	}
}

func TestAskUnavailable(t *testing.T) {
	m := New(context.Background(), &Clients{}, false, "ja")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = tm.(Model)

	// The assistant is available: the prompt stays open.
	m, _ = press(t, m, text("?"))
	tm, _ = m.Update(assistantStatusMsg{res: &genassistant.StatusResult{Available: true}})
	m = tm.(Model)
	if m.prompt != promptAsk {
		t.Fatal("the prompt was closed although the assistant is available")
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})

	// It is not: the prompt is closed and the reason is shown.
	m, _ = press(t, m, text("?"))
	reason := "the assistant is not configured"
	tm, _ = m.Update(assistantStatusMsg{res: &genassistant.StatusResult{Available: false, Reason: &reason}})
	m = tm.(Model)
	if m.prompt != promptNone {
		t.Error("the prompt is still open")
	}
	if view := ansi.Strip(m.render()); !strings.Contains(view, "アシスタントを使えません: the assistant is not configured") {
		t.Errorf("view:\n%s", view)
	}

	// A status that arrives after the prompt was closed does nothing.
	m.setStatusText(statusInfo, "")
	tm, _ = m.Update(assistantStatusMsg{res: &genassistant.StatusResult{Available: false, Reason: &reason}})
	m = tm.(Model)
	if strings.Contains(ansi.Strip(m.render()), "アシスタントを使えません") {
		t.Error("a late status was shown")
	}
}
