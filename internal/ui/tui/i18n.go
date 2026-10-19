package tui

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

// Langs are the languages of the TUI, each with i18n/<lang>.json. English is
// the base: a key missing from another language is shown in English.
var Langs = []string{"en", "ja"}

//go:embed i18n/*.json
var i18nFS embed.FS

// ValidLang reports whether lang is one of Langs.
func ValidLang(lang string) bool { return slices.Contains(Langs, lang) }

// Lang returns the language of the TUI: lang if set (the -lang flag or lang of
// the settings file, which the web UI shares), else ja when $LANG is
// Japanese, else en.
func Lang(lang string) string {
	if lang != "" {
		return lang
	}
	if strings.HasPrefix(os.Getenv("LANG"), "ja") {
		return "ja"
	}
	return "en"
}

// catalog holds the messages of a language over the English ones.
type catalog struct {
	msgs map[string]string
}

// newCatalog returns the messages of lang, falling back to English; an
// unknown lang is English.
func newCatalog(lang string) *catalog {
	c := &catalog{msgs: map[string]string{}}
	for _, l := range []string{"en", lang} {
		if l == "" || !ValidLang(l) {
			continue
		}
		b, err := i18nFS.ReadFile("i18n/" + l + ".json")
		if err != nil {
			panic(err) // embedded
		}
		var m map[string]string
		if err := json.Unmarshal(b, &m); err != nil {
			panic(fmt.Sprintf("i18n/%s.json: %v", l, err))
		}
		for k, v := range m {
			c.msgs[k] = v
		}
	}
	return c
}

// t returns the message of key with each {name} replaced by its value, given
// as pairs of a name and a value. A missing key is returned as is, so that it
// shows up.
func (c *catalog) t(key string, args ...any) string {
	s, ok := c.msgs[key]
	if !ok {
		return key
	}
	for i := 0; i+1 < len(args); i += 2 {
		s = strings.ReplaceAll(s, "{"+fmt.Sprint(args[i])+"}", fmt.Sprint(args[i+1]))
	}
	return s
}

// msgError is an error whose message is a key of the catalog, so that the UI
// shows it in its language.
type msgError struct {
	key  string
	args []any
}

func (e *msgError) Error() string { return newCatalog("en").t(e.key, e.args...) }

func errMsg(key string, args ...any) error { return &msgError{key, args} }
