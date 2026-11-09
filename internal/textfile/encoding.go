package textfile

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
)

var Codepages = []string{"UTF-8", "UTF-16LE", "UTF-16BE", "Windows-1251", "CP866", "Windows-1252", "ISO-8859-1", "KOI8-R"}

func codec(cp string) encoding.Encoding {
	switch cp {
	case "UTF-16LE":
		return unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
	case "UTF-16BE":
		return unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)
	case "Windows-1251":
		return charmap.Windows1251
	case "CP866":
		return charmap.CodePage866
	case "Windows-1252":
		return charmap.Windows1252
	case "ISO-8859-1":
		return charmap.ISO8859_1
	case "KOI8-R":
		return charmap.KOI8R
	}
	return nil
}
func Detect(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xff, 0xfe}):
		return "UTF-16LE"
	case bytes.HasPrefix(b, []byte{0xfe, 0xff}):
		return "UTF-16BE"
	case utf8.Valid(b):
		return "UTF-8"
	default:
		return "Windows-1251"
	}
}
func Decode(b []byte, cp string) (text string, bom bool, err error) {
	switch cp {
	case "UTF-8":
		if bytes.HasPrefix(b, []byte{0xef, 0xbb, 0xbf}) {
			bom = true
			b = b[3:]
		}
		if !utf8.Valid(b) {
			return "", bom, fmt.Errorf("invalid UTF-8; choose another codepage")
		}
		return string(b), bom, nil
	case "UTF-16LE":
		if bytes.HasPrefix(b, []byte{0xff, 0xfe}) {
			bom = true
			b = b[2:]
		}
		if len(b)%2 != 0 {
			return "", bom, fmt.Errorf("incomplete UTF-16 code unit")
		}
	case "UTF-16BE":
		if bytes.HasPrefix(b, []byte{0xfe, 0xff}) {
			bom = true
			b = b[2:]
		}
		if len(b)%2 != 0 {
			return "", bom, fmt.Errorf("incomplete UTF-16 code unit")
		}
	}
	enc := codec(cp)
	if enc == nil {
		return "", false, fmt.Errorf("unsupported codepage %s", cp)
	}
	result, err := enc.NewDecoder().Bytes(b)
	return string(result), bom, err
}
func Encode(text, cp string, bom bool) ([]byte, error) {
	var prefix []byte
	if bom {
		switch cp {
		case "UTF-8":
			prefix = []byte{0xef, 0xbb, 0xbf}
		case "UTF-16LE":
			prefix = []byte{0xff, 0xfe}
		case "UTF-16BE":
			prefix = []byte{0xfe, 0xff}
		}
	}
	if cp == "UTF-8" {
		return append(prefix, []byte(text)...), nil
	}
	enc := codec(cp)
	if enc == nil {
		return nil, fmt.Errorf("unsupported codepage %s", cp)
	}
	b, err := enc.NewEncoder().Bytes([]byte(text))
	if err != nil {
		return nil, fmt.Errorf("text cannot be represented in %s: %w", cp, err)
	}
	return append(prefix, b...), nil
}
func DetectEOL(s string) string {
	if strings.Contains(s, "\r\n") {
		return "CRLF"
	}
	if strings.Contains(s, "\r") {
		return "CR"
	}
	return "LF"
}
func Normalize(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}
func Endings(s, eol string) string {
	switch eol {
	case "CRLF":
		return strings.ReplaceAll(s, "\n", "\r\n")
	case "CR":
		return strings.ReplaceAll(s, "\n", "\r")
	}
	return s
}
