package main

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestEditMenu(t *testing.T) {
	menu := application.NewMenu()
	editMenu(menu.AddSubmenu("Edit"))
	translateMenu(menu, map[string]string{"Edit": "E", "Paste": "P", "Delete": "D"})
	edit := menu.ItemAt(0).GetSubmenu()
	if got := menu.ItemAt(0).Label(); got != "E" {
		t.Errorf("menu = %q, want E", got)
	}
	var labels []string
	for i := 0; edit.ItemAt(i) != nil; i++ {
		item := edit.ItemAt(i)
		if item.IsSeparator() {
			continue
		}
		labels = append(labels, item.Label())
		if acc := item.GetAccelerator(); acc != "" {
			t.Errorf("%q has the shortcut %q", item.Label(), acc)
		}
	}
	want := []string{"Undo\tCtrl+Z", "Redo\tCtrl+Y", "Cut\tCtrl+X", "Copy\tCtrl+C", "P\tCtrl+V", "D", "Select All\tCtrl+A"}
	if len(labels) != len(want) {
		t.Fatalf("labels = %q, want %q", labels, want)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Errorf("labels[%d] = %q, want %q", i, labels[i], want[i])
		}
	}
}
