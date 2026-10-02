package html

import (
	"fmt"
	"strconv"
	"unicode/utf8"
)

// appendUpdateJSON appends the canonical JSON encoding of update to dst. The
// bytes are identical to encoding/json.Marshal(update) (including HTML
// escaping of text), but it writes into one caller-sized buffer instead of
// going through reflection and an intermediate copy.
func appendUpdateJSON(dst []byte, update Update) ([]byte, error) {
	dst = append(dst, `{"schemaVersion":`...)
	dst = strconv.AppendUint(dst, uint64(update.SchemaVersion), 10)
	dst = append(dst, `,"width":`...)
	dst = strconv.AppendInt(dst, int64(update.Width), 10)
	dst = append(dst, `,"height":`...)
	dst = strconv.AppendInt(dst, int64(update.Height), 10)
	dst = append(dst, `,"snapshot":`...)
	dst = strconv.AppendBool(dst, update.Snapshot)
	dst = append(dst, `,"rows":`...)
	if update.Rows == nil {
		dst = append(dst, "null"...)
	} else {
		dst = append(dst, '[')
		for i := range update.Rows {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendRowJSON(dst, &update.Rows[i])
		}
		dst = append(dst, ']')
	}
	dst = append(dst, `,"styles":`...)
	if update.Styles == nil {
		dst = append(dst, "null"...)
	} else {
		dst = append(dst, '[')
		for i := range update.Styles {
			if i > 0 {
				dst = append(dst, ',')
			}
			var err error
			if dst, err = appendStyleJSON(dst, &update.Styles[i]); err != nil {
				return nil, err
			}
		}
		dst = append(dst, ']')
	}
	dst = append(dst, `,"cursor":{"row":`...)
	dst = strconv.AppendInt(dst, int64(update.Cursor.Row), 10)
	dst = append(dst, `,"column":`...)
	dst = strconv.AppendInt(dst, int64(update.Cursor.Column), 10)
	dst = append(dst, `,"visible":`...)
	dst = strconv.AppendBool(dst, update.Cursor.Visible)
	dst = append(dst, `,"style":`...)
	dst = strconv.AppendUint(dst, uint64(update.Cursor.Style), 10)
	dst = append(dst, `,"styleSet":`...)
	dst = strconv.AppendBool(dst, update.Cursor.StyleSet)
	dst = append(dst, "}}"...)
	return dst, nil
}

func appendRowJSON(dst []byte, row *RowUpdate) []byte {
	dst = append(dst, `{"row":`...)
	dst = strconv.AppendInt(dst, int64(row.Row), 10)
	dst = append(dst, `,"cells":`...)
	if row.Cells == nil {
		return append(dst, "null}"...)
	}
	dst = append(dst, '[')
	for i := range row.Cells {
		cell := &row.Cells[i]
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = append(dst, `{"column":`...)
		dst = strconv.AppendInt(dst, int64(cell.Column), 10)
		dst = append(dst, `,"width":`...)
		dst = strconv.AppendInt(dst, int64(cell.Width), 10)
		dst = append(dst, `,"text":`...)
		dst = appendJSONString(dst, cell.Text)
		dst = append(dst, `,"style":`...)
		dst = strconv.AppendInt(dst, int64(cell.Style), 10)
		dst = append(dst, '}')
	}
	return append(dst, "]}"...)
}

func appendStyleJSON(dst []byte, style *Style) ([]byte, error) {
	dst = append(dst, '{')
	first := true
	flag := func(name string, set bool) {
		if !set {
			return
		}
		if !first {
			dst = append(dst, ',')
		}
		first = false
		dst = append(dst, '"')
		dst = append(dst, name...)
		dst = append(dst, `":true`...)
	}
	flag("bold", style.Bold)
	flag("italic", style.Italic)
	flag("inverse", style.Inverse)
	flag("dim", style.Dim)
	flag("blink", style.Blink)
	flag("strikethrough", style.Strikethrough)
	flag("underline", style.Underline)
	if style.UnderlineStyle != 0 {
		if !first {
			dst = append(dst, ',')
		}
		first = false
		dst = append(dst, `"underlineStyle":`...)
		dst = strconv.AppendUint(dst, uint64(style.UnderlineStyle), 10)
	}
	if !first {
		dst = append(dst, ',')
	}
	var err error
	dst = append(dst, `"foreground":`...)
	if dst, err = appendColorJSON(dst, style.Foreground); err != nil {
		return nil, err
	}
	dst = append(dst, `,"background":`...)
	if dst, err = appendColorJSON(dst, style.Background); err != nil {
		return nil, err
	}
	dst = append(dst, `,"underlineColor":`...)
	if dst, err = appendColorJSON(dst, style.UnderlineColor); err != nil {
		return nil, err
	}
	return append(dst, '}'), nil
}

// appendColorJSON mirrors Color.MarshalJSON.
func appendColorJSON(dst []byte, color Color) ([]byte, error) {
	switch color.Kind {
	case ColorDefault:
		return append(dst, `{"kind":0}`...), nil
	case ColorIndexed:
		dst = append(dst, `{"kind":1,"index":`...)
		dst = strconv.AppendUint(dst, uint64(color.Index), 10)
		return append(dst, '}'), nil
	case ColorRGB:
		dst = append(dst, `{"kind":2,"rgb":{"r":`...)
		dst = strconv.AppendUint(dst, uint64(color.RGB.R), 10)
		dst = append(dst, `,"g":`...)
		dst = strconv.AppendUint(dst, uint64(color.RGB.G), 10)
		dst = append(dst, `,"b":`...)
		dst = strconv.AppendUint(dst, uint64(color.RGB.B), 10)
		return append(dst, "}}"...), nil
	default:
		return nil, fmt.Errorf("html: invalid color kind %d", color.Kind)
	}
}

const hexDigits = "0123456789abcdef"

// appendJSONString appends s as a JSON string using encoding/json's default
// escaping: quotes, backslashes, control characters, <, >, & and U+2028/2029
// are escaped. Cell text is always valid UTF-8; an invalid byte, should one
// appear, is replaced by U+FFFD so the output stays valid JSON.
func appendJSONString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(s); {
		b := s[i]
		if b < utf8.RuneSelf {
			if b >= 0x20 && b != '"' && b != '\\' && b != '<' && b != '>' && b != '&' {
				i++
				continue
			}
			dst = append(dst, s[start:i]...)
			switch b {
			case '"', '\\':
				dst = append(dst, '\\', b)
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\f':
				dst = append(dst, '\\', 'f')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			default:
				dst = append(dst, '\\', 'u', '0', '0', hexDigits[b>>4], hexDigits[b&0xF])
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			dst = append(dst, s[start:i]...)
			dst = append(dst, "\uFFFD"...)
			i += size
			start = i
			continue
		}
		if r == '\u2028' || r == '\u2029' {
			dst = append(dst, s[start:i]...)
			dst = append(dst, '\\', 'u', '2', '0', '2', hexDigits[r&0xF])
			i += size
			start = i
			continue
		}
		i += size
	}
	dst = append(dst, s[start:]...)
	return append(dst, '"')
}

// estimateUpdateJSONSize returns a capacity hint close to the encoded size so
// the output buffer normally grows zero times.
func estimateUpdateJSONSize(update Update) int {
	size := 256 + 256*len(update.Styles)
	for i := range update.Rows {
		size += 32 + 48*len(update.Rows[i].Cells)
	}
	return size
}
