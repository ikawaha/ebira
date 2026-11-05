package main

/*
#include <stdlib.h>
char *preferredLanguage(void);
void setDialogLabels(const char *ok, const char *cancel);
*/
import "C"

import (
	"embed"
	"encoding/json"
	"strings"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The catalogs map the English text to the text of a language. Text without an
// entry stays in English. The labels of the menu roles come from Wails.
//
//go:embed i18n/*.json
var catalogs embed.FS

func language() string {
	p := C.preferredLanguage()
	defer C.free(unsafe.Pointer(p))
	lang, _, _ := strings.Cut(C.GoString(p), "-")
	return lang
}

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
		if label, ok := messages[item.Label()]; ok {
			item.SetLabel(label)
		}
		if sub := item.GetSubmenu(); sub != nil {
			translateMenu(sub, messages)
		}
	}
}

func translateDialogs(messages map[string]string) {
	text := func(s string) *C.char {
		if t, ok := messages[s]; ok {
			s = t
		}
		return C.CString(s) // kept by the dialogs for the life of the app
	}
	C.setDialogLabels(text("OK"), text("Cancel"))
}
