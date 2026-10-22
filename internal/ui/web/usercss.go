package web

import (
	"strings"
	"unicode/utf8"
)

// PreviewCSSFile is the name of the file of CSS for the preview, next to the
// settings file.
const PreviewCSSFile = "preview.css"

// previewScope is the selector the CSS for the preview is nested in, so that
// it styles the body of a note, in the preview and in the preview of the
// editor, and nothing else of the page.
const previewScope = "#preview .body"

// maxPreviewCSS is the size of the CSS for the preview read at most.
const maxPreviewCSS = 256 << 10

// urlFunctions are the functions that take a URL, quoted or not, which would
// make the browser fetch it.
var urlFunctions = map[string]bool{
	"url": true, "src": true, "image": true, "image-set": true, "-webkit-image-set": true,
	"cross-fade": true, "-webkit-cross-fade": true, "element": true, "-moz-element": true,
}

// PreviewCSS returns css, written by the user to style the preview, as served
// to the page: nested in the selector of the body of a note, and without what
// could make the browser fetch something, which CSS could use to send the
// content of the page out, such as with attribute selectors and a URL for
// each value:
//
//   - @import, and at-rules whose name has an escape;
//   - url() but with a data: URL, and the other functions that take a URL,
//     such as image-set(), but url() and src() with a data: URL;
//   - functions whose name has an escape, which may spell one of those.
//
// A } that would close the selector it is nested in is dropped, and blocks
// left open are closed.
func PreviewCSS(css string) string {
	if len(css) > maxPreviewCSS {
		css = css[:maxPreviewCSS]
	}
	css = strings.ToValidUTF8(css, "�")
	toks := lexCSS(css)
	var b strings.Builder
	b.WriteString(previewScope + " {\n")
	depth := 0
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch t.kind {
		case tokAtKeyword:
			if t.escaped || t.name == "import" || t.name == "charset" {
				i = skipRule(toks, i)
				continue
			}
		case tokURL:
			if !isDataURL(t.value) {
				continue
			}
		case tokFunction:
			if t.escaped || urlFunctions[t.name] {
				if (t.name == "url" || t.name == "src") && !t.escaped && dataArg(toks, i) {
					break
				}
				i = skipBlock(toks, i)
				continue
			}
		case tokOpen:
			if t.text == "{" {
				depth++
			}
		case tokClose:
			if t.text == "}" {
				if depth == 0 {
					continue
				}
				depth--
			}
		}
		b.WriteString(t.text)
	}
	for ; depth > 0; depth-- {
		b.WriteString("}")
	}
	b.WriteString("\n}\n")
	return b.String()
}

// isDataURL reports whether u, the content of url() or a string, is a data:
// URL. One with an escape is not, as the escape may hide another scheme.
func isDataURL(u string) bool {
	u = strings.TrimSpace(u)
	return !strings.Contains(u, `\`) && len(u) >= 5 && strings.EqualFold(u[:5], "data:")
}

// dataArg reports whether the function at i takes one string, a data: URL.
func dataArg(toks []cssToken, i int) bool {
	var args []cssToken
	for j := i + 1; j < len(toks); j++ {
		switch t := toks[j]; {
		case t.kind == tokSpace:
		case t.kind == tokClose && t.text == ")":
			return len(args) == 1 && args[0].kind == tokString && isDataURL(args[0].value)
		default:
			args = append(args, t)
		}
	}
	return false
}

// skipBlock returns the index of the ) that closes the function or the
// parenthesis at i, or the last token.
func skipBlock(toks []cssToken, i int) int {
	depth := 0
	for ; i < len(toks); i++ {
		switch t := toks[i]; {
		case t.kind == tokFunction || t.kind == tokOpen && t.text == "(":
			depth++
		case t.kind == tokClose && t.text == ")":
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return len(toks) - 1
}

// skipRule returns the index of the end of the at-rule at i: its ;, the }
// of its block, or the } that closes the block it is in (which is kept), or
// the last token.
func skipRule(toks []cssToken, i int) int {
	depth := 0
	for j := i + 1; j < len(toks); j++ {
		t := toks[j]
		switch {
		case t.kind == tokOpen && t.text == "{":
			depth++
		case t.kind == tokClose && t.text == "}":
			if depth == 0 {
				return j - 1
			}
			if depth--; depth == 0 {
				return j
			}
		case t.text == ";" && depth == 0:
			return j
		}
	}
	return len(toks) - 1
}

type tokKind int

const (
	tokOther tokKind = iota
	tokSpace
	tokString
	tokIdent
	tokFunction  // name(
	tokAtKeyword // @name
	tokURL       // url(unquoted)
	tokOpen      // { ( [
	tokClose     // } ) ]
)

// cssToken is a token of CSS: its text as written, and for names the name in
// lower case and whether it had an escape, for strings and url() the value.
type cssToken struct {
	kind    tokKind
	text    string
	name    string
	value   string
	escaped bool
}

// lexCSS splits css into tokens, after CSS Syntax Module Level 3 as far as
// PreviewCSS needs: comments (dropped), strings, names with their escapes,
// functions, at-keywords, url() and brackets. Everything else is passed on a
// rune at a time.
func lexCSS(s string) []cssToken {
	var toks []cssToken
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case strings.HasPrefix(s[i:], "/*"):
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				i = len(s)
			} else {
				i += 2 + end + 2
			}
			toks = append(toks, cssToken{kind: tokSpace, text: " "})
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
			j := i
			for j < len(s) && strings.IndexByte(" \t\n\r\f", s[j]) >= 0 {
				j++
			}
			toks = append(toks, cssToken{kind: tokSpace, text: s[i:j]})
			i = j
		case c == '"' || c == '\'':
			j, value := lexString(s, i)
			toks = append(toks, cssToken{kind: tokString, text: s[i:j], value: value})
			i = j
		case c == '@' && startsName(s, i+1):
			j, name, esc := lexName(s, i+1)
			toks = append(toks, cssToken{kind: tokAtKeyword, text: s[i:j], name: name, escaped: esc})
			i = j
		case startsName(s, i):
			j, name, esc := lexName(s, i)
			if j < len(s) && s[j] == '(' {
				j++
				if name == "url" && !esc {
					if k, value, ok := lexURL(s, j); ok {
						toks = append(toks, cssToken{kind: tokURL, text: s[i:k], name: name, value: value})
						i = k
						continue
					}
				}
				toks = append(toks, cssToken{kind: tokFunction, text: s[i:j], name: name, escaped: esc})
			} else {
				toks = append(toks, cssToken{kind: tokIdent, text: s[i:j], name: name, escaped: esc})
			}
			i = j
		case c == '{' || c == '(' || c == '[':
			toks = append(toks, cssToken{kind: tokOpen, text: s[i : i+1]})
			i++
		case c == '}' || c == ')' || c == ']':
			toks = append(toks, cssToken{kind: tokClose, text: s[i : i+1]})
			i++
		case c == '\\':
			// A backslash that starts no name, such as before a newline.
			toks = append(toks, cssToken{kind: tokOther, text: `\`})
			i++
		default:
			_, size := utf8.DecodeRuneInString(s[i:])
			toks = append(toks, cssToken{kind: tokOther, text: s[i : i+size]})
			i += size
		}
	}
	return toks
}

// lexString returns the end of the string that starts at i, and its value.
// A string ends at its quote, or before a newline (a bad string) or at the
// end.
func lexString(s string, i int) (int, string) {
	q := s[i]
	var v strings.Builder
	j := i + 1
	for j < len(s) {
		switch c := s[j]; {
		case c == q:
			return j + 1, v.String()
		case c == '\n':
			return j, v.String()
		case c == '\\' && j+1 < len(s):
			v.WriteString(s[j : j+2]) // kept escaped: a value with one is not trusted
			j += 2
		default:
			v.WriteByte(c)
			j++
		}
	}
	return j, v.String()
}

// lexURL reads the unquoted URL of url( ending at j, and returns its end,
// past the ), and its value; ok is false when the URL is quoted, which
// makes url( a function taking a string.
func lexURL(s string, j int) (end int, value string, ok bool) {
	k := j
	for k < len(s) && strings.IndexByte(" \t\n\r\f", s[k]) >= 0 {
		k++
	}
	if k < len(s) && (s[k] == '"' || s[k] == '\'') {
		return 0, "", false
	}
	for k < len(s) && s[k] != ')' {
		if s[k] == '\\' && k+1 < len(s) {
			k++
		}
		k++
	}
	value = s[j:k]
	if k < len(s) {
		k++
	}
	return k, value, true
}

// startsName reports whether a name starts at i: a letter, _, a character
// not ASCII or an escape, or a - followed by one of those or another -.
func startsName(s string, i int) bool {
	if i >= len(s) {
		return false
	}
	if s[i] == '-' {
		return i+1 < len(s) && (s[i+1] == '-' || nameStart(s[i+1]) || isEscape(s, i+1))
	}
	return nameStart(s[i]) || isEscape(s, i)
}

func nameStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c >= 0x80
}

func nameChar(c byte) bool { return nameStart(c) || c >= '0' && c <= '9' || c == '-' }

// isEscape reports whether a valid escape starts at i: a backslash not
// before a newline.
func isEscape(s string, i int) bool {
	return i+1 < len(s) && s[i] == '\\' && s[i+1] != '\n' && s[i+1] != '\r' && s[i+1] != '\f'
}

// lexName returns the end of the name that starts at i, the name in lower
// case, and whether it has an escape.
func lexName(s string, i int) (int, string, bool) {
	j, esc := i, false
	for j < len(s) {
		switch {
		case isEscape(s, j):
			esc = true
			j += 2
		case nameChar(s[j]):
			j++
		default:
			return j, strings.ToLower(s[i:j]), esc
		}
	}
	return j, strings.ToLower(s[i:j]), esc
}
