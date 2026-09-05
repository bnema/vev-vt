package vt

import (
	"encoding/binary"
	"math"
	"unicode/utf8"

	renderer "github.com/bnema/vev-vt/core"
)

func appendHistoryStyle(dst []byte, style renderer.Style) []byte {
	style = style.Canonical()
	flags := byte(0)
	for i, set := range [...]bool{style.Bold, style.Italic, style.Inverse, style.HasForegroundRGB, style.HasBackgroundRGB, style.HasUnderlineColor, style.HasUnderlineColorRGB} {
		if set {
			flags |= 1 << i
		}
	}
	dst = append(dst, flags)
	dst = binary.BigEndian.AppendUint16(dst, uint16(style.Attrs))
	dst = binary.BigEndian.AppendUint64(dst, uint64(int64(style.Foreground)))
	dst = binary.BigEndian.AppendUint64(dst, uint64(int64(style.Background)))
	dst = append(dst, style.ForegroundRGB.R, style.ForegroundRGB.G, style.ForegroundRGB.B)
	dst = append(dst, style.BackgroundRGB.R, style.BackgroundRGB.G, style.BackgroundRGB.B)
	dst = append(dst, byte(style.UnderlineStyle))
	dst = binary.BigEndian.AppendUint64(dst, uint64(int64(style.UnderlineColor)))
	return append(dst, style.UnderlineColorRGB.R, style.UnderlineColorRGB.G, style.UnderlineColorRGB.B)
}

func appendHistoryBound(dst []byte, bound LineBound) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(bound.End))
	if bound.Soft {
		return append(dst, 1)
	}
	return append(dst, 0)
}

type historyParser struct{ data []byte }

func (p *historyParser) uint32() (uint32, bool) {
	if len(p.data) < 4 {
		return 0, false
	}
	value := binary.BigEndian.Uint32(p.data)
	p.data = p.data[4:]
	return value, true
}

func (p *historyParser) uint64() (uint64, bool) {
	if len(p.data) < 8 {
		return 0, false
	}
	value := binary.BigEndian.Uint64(p.data)
	p.data = p.data[8:]
	return value, true
}

func (p *historyParser) style() (renderer.Style, bool) {
	if len(p.data) < historyStyleBytes {
		return renderer.Style{}, false
	}
	b := p.data[:historyStyleBytes]
	p.data = p.data[historyStyleBytes:]
	flags := b[0]
	if flags&0x80 != 0 {
		return renderer.Style{}, false
	}
	foreground, fok := historyInt(binary.BigEndian.Uint64(b[3:11]))
	background, bok := historyInt(binary.BigEndian.Uint64(b[11:19]))
	underline, uok := historyInt(binary.BigEndian.Uint64(b[26:34]))
	if !fok || !bok || !uok {
		return renderer.Style{}, false
	}
	return renderer.Style{
		Bold:                 flags&1 != 0,
		Italic:               flags&2 != 0,
		Inverse:              flags&4 != 0,
		Attrs:                renderer.StyleAttrs(binary.BigEndian.Uint16(b[1:3])),
		Foreground:           foreground,
		Background:           background,
		HasForegroundRGB:     flags&8 != 0,
		ForegroundRGB:        renderer.RGB{R: b[19], G: b[20], B: b[21]},
		HasBackgroundRGB:     flags&16 != 0,
		BackgroundRGB:        renderer.RGB{R: b[22], G: b[23], B: b[24]},
		UnderlineStyle:       renderer.UnderlineStyle(b[25]),
		HasUnderlineColor:    flags&32 != 0,
		UnderlineColor:       underline,
		HasUnderlineColorRGB: flags&64 != 0,
		UnderlineColorRGB:    renderer.RGB{R: b[34], G: b[35], B: b[36]},
	}, true
}

func (p *historyParser) bound() (LineBound, bool) {
	if len(p.data) < historyBoundBytes {
		return LineBound{}, false
	}
	end := binary.BigEndian.Uint32(p.data[:4])
	flag := p.data[4]
	p.data = p.data[historyBoundBytes:]
	if flag > 1 || uint64(end) > uint64(math.MaxInt) {
		return LineBound{}, false
	}
	return LineBound{End: int(end), Soft: flag == 1}, true
}

func (p *historyParser) storedCell(styles, payloads uint32) (rune, uint32, uint32, bool, bool) {
	if len(p.data) < historyStoredCellBytes {
		return 0, 0, 0, false, false
	}
	b := p.data[:historyStoredCellBytes]
	p.data = p.data[historyStoredCellBytes:]
	r := rune(binary.BigEndian.Uint32(b[:4]))
	id := binary.BigEndian.Uint32(b[4:8])
	if !utf8.ValidRune(r) || id >= styles || b[8] > 3 {
		return 0, 0, 0, false, false
	}
	var payloadID uint32
	if b[8]&2 != 0 {
		var ok bool
		payloadID, ok = p.uint32()
		if !ok || payloadID == 0 || payloadID > payloads {
			return 0, 0, 0, false, false
		}
	}
	return r, id, payloadID, b[8]&1 != 0, true
}

func historyInt(raw uint64) (int, bool) {
	value := int64(raw)
	return int(value), int64(int(value)) == value
}

func validHistoryCell(cell renderer.Cell) bool {
	return utf8.ValidRune(cell.Rune) && cell.Style.UnderlineStyle <= renderer.UnderlineDashed
}
