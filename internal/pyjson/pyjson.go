// Package pyjson encodes JSON the way CPython's json.dumps does with default
// settings, so the Go hook emits bytes identical to the Python hook it replaces.
//
// Go's encoding/json differs from json.dumps in three ways that all show up in
// this hook's output:
//
//	separators   json.dumps writes ", " and ": "; Go writes "," and ":".
//	ensure_ascii json.dumps escapes every non-ASCII rune as \uXXXX; Go emits
//	             raw UTF-8. The NUDGE string contains an em dash, so this is
//	             not hypothetical.
//	HTML escaping Go rewrites <, > and & as <, > and & unless
//	             explicitly disabled; json.dumps leaves them alone. The
//	             DOCTRINE string is wrapped in <oracle-plugin> tags.
//
// Any one of those would make the two implementations disagree byte for byte
// while agreeing semantically, which would leave the differential test unable
// to tell cosmetic drift from a real behavior change. Encoding here rather
// than post-processing encoding/json's output also avoids corrupting string
// contents that happen to contain ", " or ": ".
//
// Only the shapes this hook actually emits are supported. Anything else is a
// programming error and returns one.
package pyjson

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"unicode/utf8"
)

// Member is one key/value pair of an Object.
type Member struct {
	Key   string
	Value any
}

// Object is a JSON object with a fixed member order. Go maps iterate in
// random order, and a Python dict preserves insertion order, so the output
// order has to be stated explicitly for the two to match.
type Object []Member

// ErrUnsupported is returned for a value kind this encoder does not handle.
var ErrUnsupported = errors.New("pyjson: unsupported value")

// Marshal encodes v as CPython's json.dumps would with default settings.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := encode(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encode(buf *bytes.Buffer, v any) error {
	switch val := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if val {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case string:
		EncodeString(buf, val)
	case int:
		buf.WriteString(strconv.Itoa(val))
	case int64:
		buf.WriteString(strconv.FormatInt(val, 10))
	case Object:
		return encodeObject(buf, val)
	case map[string]any:
		// Accepted for convenience, with keys sorted so the output is at least
		// deterministic. Prefer Object when the order has to match Python's.
		members := make(Object, 0, len(val))
		for k := range val {
			members = append(members, Member{Key: k})
		}
		sort.Slice(members, func(i, j int) bool { return members[i].Key < members[j].Key })
		for i := range members {
			members[i].Value = val[members[i].Key]
		}
		return encodeObject(buf, members)
	case []any:
		buf.WriteByte('[')
		for i, item := range val {
			if i > 0 {
				buf.WriteString(", ")
			}
			if err := encode(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	default:
		return fmt.Errorf("%w: %T", ErrUnsupported, v)
	}
	return nil
}

func encodeObject(buf *bytes.Buffer, obj Object) error {
	buf.WriteByte('{')
	for i, m := range obj {
		if i > 0 {
			buf.WriteString(", ") // json.dumps default item separator
		}
		EncodeString(buf, m.Key)
		buf.WriteString(": ") // json.dumps default key separator
		if err := encode(buf, m.Value); err != nil {
			return err
		}
	}
	buf.WriteByte('}')
	return nil
}

// EncodeString writes s as a quoted JSON string using CPython's
// ensure_ascii=True rules.
//
// CPython's ESCAPE_ASCII pattern is ([\\"]|[^\ -~]): a character is escaped
// when it is a backslash, a double quote, or falls outside the printable ASCII
// range U+0020..U+007E. Note that U+007F (DEL) is outside that range and so IS
// escaped, which a "> 0x7F" test would miss.
func EncodeString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			buf.WriteString(`\\`)
		case '"':
			buf.WriteString(`\"`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			switch {
			case r >= 0x20 && r <= 0x7e:
				buf.WriteByte(byte(r))
			case r == utf8.RuneError:
				// A byte that was not valid UTF-8. Python's decoder would have
				// produced U+FFFD here too; emit its escape.
				writeHex(buf, 0xfffd)
			case r <= 0xffff:
				writeHex(buf, uint32(r))
			default:
				// Non-BMP: CPython emits a UTF-16 surrogate pair.
				r -= 0x10000
				writeHex(buf, uint32(0xd800+(r>>10)))
				writeHex(buf, uint32(0xdc00+(r&0x3ff)))
			}
		}
	}
	buf.WriteByte('"')
}

const hexDigits = "0123456789abcdef"

func writeHex(buf *bytes.Buffer, r uint32) {
	buf.WriteString(`\u`)
	buf.WriteByte(hexDigits[(r>>12)&0xf])
	buf.WriteByte(hexDigits[(r>>8)&0xf])
	buf.WriteByte(hexDigits[(r>>4)&0xf])
	buf.WriteByte(hexDigits[r&0xf])
}
