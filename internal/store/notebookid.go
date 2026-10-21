package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// maxIDLen is the longest note or notebook ID, as nameRe allows.
const maxIDLen = 64

// NotebookID derives a notebook ID, which is also the directory name, from a
// display name. The ID uses only the characters an ID allows (a-z, 0-9, - and
// _), so the directory name stays ASCII while the display name, kept in
// _notebook.yaml, can be in any language.
//
//   - The name is NFC-normalized and lower-cased, and runs of spaces,
//     punctuation and other characters an ID cannot have become "-".
//   - A name left with only ASCII is used as is: "My Notes" gives "my-notes".
//   - Otherwise it is Punycode-encoded as in internationalized domain names:
//     a Japanese name gives an ID such as "xn--4dkc0654c57za".
//   - A result longer than an ID allows is cut and ends with a hash of the name.
//
// The ID may still collide with an existing or reserved notebook; see
// UniqueNotebookID.
func NotebookID(name string) string {
	var sb strings.Builder
	dash := false
	for _, r := range strings.ToLower(norm.NFC.String(name)) {
		keep := r == '_' || r == '-' || ('a' <= r && r <= 'z') || ('0' <= r && r <= '9') ||
			(r >= utf8.RuneSelf && !unicode.IsSpace(r) && !unicode.IsPunct(r) && !unicode.IsControl(r))
		if !keep || r == '-' {
			dash = sb.Len() > 0
			continue
		}
		if dash {
			sb.WriteByte('-')
			dash = false
		}
		sb.WriteRune(r)
	}
	s := strings.TrimLeft(sb.String(), "_-") // an ID starts with a letter or digit
	if s == "" {
		s = "notebook"
	}
	for _, r := range s {
		if r >= utf8.RuneSelf {
			s = "xn--" + punycode(s)
			break
		}
	}
	if len(s) > maxIDLen {
		h := sha256.Sum256([]byte(name))
		s = s[:maxIDLen-9] + "-" + hex.EncodeToString(h[:4])
	}
	return s
}

// NotebookName restores a display name from a notebook ID made by NotebookID,
// for a notebook without a name in _notebook.yaml. A Punycode ID ("xn--...")
// gives the decoded name, and one with a suffix added by UniqueNotebookID gives
// the name followed by the number in parentheses, such as "name (2)". The name is
// lower-cased and has "-" in place of spaces and punctuation, as NotebookID
// made them. Any other ID, including one cut for its length, is returned as is.
// A sub notebook is named after its own part of the ID.
func NotebookName(id string) string {
	if i := strings.LastIndexAny(id, "/"+subSep); i >= 0 {
		id = id[i+1:]
	}
	if name, ok := decodeNotebookID(id); ok {
		return name
	}
	if i := strings.LastIndexByte(id, '-'); i > 0 && i+1 < len(id) && id[i+1] != '0' {
		if n, err := strconv.Atoi(id[i+1:]); err == nil && n >= 2 {
			if name, ok := decodeNotebookID(id[:i]); ok {
				return name + " (" + strconv.Itoa(n) + ")"
			}
		}
	}
	return id
}

// decodeNotebookID decodes a Punycode notebook ID, accepting the result only
// when NotebookID gives the ID back.
func decodeNotebookID(id string) (string, bool) {
	p, ok := strings.CutPrefix(id, "xn--")
	if !ok || truncatedID(id) {
		return "", false
	}
	name, err := punydecode(p)
	if err != nil || NotebookID(name) != id {
		return "", false
	}
	return name, true
}

// truncatedID reports whether id looks cut by NotebookID for its length: the
// longest ID ending with "-" and a hash in 8 hex digits. Such an ID can still
// decode, but to a meaningless name.
func truncatedID(id string) bool {
	if len(id) != maxIDLen || id[maxIDLen-9] != '-' {
		return false
	}
	_, err := hex.DecodeString(id[maxIDLen-8:])
	return err == nil
}

// UniqueNotebookID returns id, or when taken reports it as used, id with the
// smallest suffix "-2", "-3", ... that is free, still within the ID length.
func UniqueNotebookID(id string, taken func(string) bool) string {
	if !taken(id) {
		return id
	}
	for i := 2; ; i++ {
		suffix := "-" + strconv.Itoa(i)
		base := id
		if len(base)+len(suffix) > maxIDLen {
			base = base[:maxIDLen-len(suffix)]
		}
		if c := base + suffix; !taken(c) {
			return c
		}
	}
}

// punycode encodes s as in RFC 3492 (without the "xn--" prefix). The ASCII
// characters of s are copied first, followed by "-" and the encoded others in
// a-z and 0-9.
func punycode(s string) string {
	const (
		base        = 36
		tMin        = 1
		tMax        = 26
		skew        = 38
		damp        = 700
		initialBias = 72
		initialN    = 128
	)
	digit := func(d int) byte {
		if d < 26 {
			return byte('a' + d)
		}
		return byte('0' + d - 26)
	}
	adapt := func(delta, numPoints int, first bool) int {
		if first {
			delta /= damp
		} else {
			delta /= 2
		}
		delta += delta / numPoints
		k := 0
		for delta > ((base-tMin)*tMax)/2 {
			delta /= base - tMin
			k += base
		}
		return k + (base-tMin+1)*delta/(delta+skew)
	}

	runes := []rune(s)
	var out []byte
	for _, r := range runes {
		if r < initialN {
			out = append(out, byte(r))
		}
	}
	b := len(out)
	h := b
	if b > 0 {
		out = append(out, '-')
	}
	n, delta, bias := initialN, 0, initialBias
	for h < len(runes) {
		m := rune(0x10FFFF + 1)
		for _, r := range runes {
			if int(r) >= n && r < m {
				m = r
			}
		}
		delta += (int(m) - n) * (h + 1)
		n = int(m)
		for _, r := range runes {
			if int(r) < n {
				delta++
				continue
			}
			if int(r) > n {
				continue
			}
			q := delta
			for k := base; ; k += base {
				t := k - bias
				if t < tMin {
					t = tMin
				} else if t > tMax {
					t = tMax
				}
				if q < t {
					break
				}
				out = append(out, digit(t+(q-t)%(base-t)))
				q = (q - t) / (base - t)
			}
			out = append(out, digit(q))
			bias = adapt(delta, h+1, h == b)
			delta = 0
			h++
		}
		delta++
		n++
	}
	return string(out)
}

var errPunycode = errors.New("invalid punycode")

// punydecode decodes s as in RFC 3492 (without the "xn--" prefix).
func punydecode(s string) (string, error) {
	const (
		base        = 36
		tMin        = 1
		tMax        = 26
		skew        = 38
		damp        = 700
		initialBias = 72
		initialN    = 128
		maxInt      = 1<<31 - 1
	)
	adapt := func(delta, numPoints int, first bool) int {
		if first {
			delta /= damp
		} else {
			delta /= 2
		}
		delta += delta / numPoints
		k := 0
		for delta > ((base-tMin)*tMax)/2 {
			delta /= base - tMin
			k += base
		}
		return k + (base-tMin+1)*delta/(delta+skew)
	}
	var out []rune
	pos := 0
	if j := strings.LastIndexByte(s, '-'); j >= 0 {
		for _, c := range []byte(s[:j]) {
			if c >= initialN {
				return "", errPunycode
			}
			out = append(out, rune(c))
		}
		pos = j + 1
	}
	n, bias, i := initialN, initialBias, 0
	for pos < len(s) {
		oldi, w := i, 1
		for k := base; ; k += base {
			if pos >= len(s) {
				return "", errPunycode
			}
			c := s[pos]
			pos++
			var d int
			switch {
			case 'a' <= c && c <= 'z':
				d = int(c - 'a')
			case 'A' <= c && c <= 'Z':
				d = int(c - 'A')
			case '0' <= c && c <= '9':
				d = int(c-'0') + 26
			default:
				return "", errPunycode
			}
			if d > (maxInt-i)/w {
				return "", errPunycode
			}
			i += d * w
			t := k - bias
			if t < tMin {
				t = tMin
			} else if t > tMax {
				t = tMax
			}
			if d < t {
				break
			}
			if w > maxInt/(base-t) {
				return "", errPunycode
			}
			w *= base - t
		}
		bias = adapt(i-oldi, len(out)+1, oldi == 0)
		if i/(len(out)+1) > maxInt-n {
			return "", errPunycode
		}
		n += i / (len(out) + 1)
		i %= len(out) + 1
		if n > unicode.MaxRune || (0xD800 <= n && n <= 0xDFFF) {
			return "", errPunycode
		}
		out = append(out[:i], append([]rune{rune(n)}, out[i:]...)...)
		i++
	}
	return string(out), nil
}
