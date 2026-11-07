package main

import (
	"embed"
	"encoding/json"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The catalogs map the English text to the text of a language. Text without an
// entry stays in English. The labels of the menu roles come from Wails.
//
//go:embed i18n/*.json
var catalogs embed.FS

func catalog(lang string) map[string]string {
	messages := map[string]string{}
	data, err := catalogs.ReadFile("i18n/" + lang + ".json")
	if err != nil {
		return messages
	}
	if err := json.Unmarshal(data, &messages); err != nil {
		panic(err) // embedded: a broken file is a broken build
	}
	return messages
}

func translateMenu(menu *application.Menu, messages map[string]string) {
	for i := 0; ; i++ {
		item := menu.ItemAt(i)
		if item == nil {
			return
		}
		// A label may show a key after a tab (editMenu).
		text, key, hasKey := strings.Cut(item.Label(), "\t")
		if label, ok := messages[text]; ok {
			if hasKey {
				label += "\t" + key
			}
			item.SetLabel(label)
		}
		if sub := item.GetSubmenu(); sub != nil {
			translateMenu(sub, messages)
		}
	}
}
