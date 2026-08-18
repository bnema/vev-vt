package ansi

import (
	"bytes"
	"strconv"
)

// writeStyle emits a canonical output style without making capability
// decisions. Those belong to styleProjector.
func writeStyle(out *bytes.Buffer, style outputStyle) {
	out.WriteString("\x1b[0")
	if style.bold {
		out.WriteString(";1")
	}
	if style.attrs&AttrDim != 0 {
		out.WriteString(";2")
	}
	if style.italic {
		out.WriteString(";3")
	}
	if style.attrs&AttrUnderline != 0 {
		switch style.underlineStyle {
		case UnderlineDouble:
			out.WriteString(";21")
		case UnderlineCurly:
			out.WriteString(";4:3")
		case UnderlineDotted:
			out.WriteString(";4:4")
		case UnderlineDashed:
			out.WriteString(";4:5")
		default:
			out.WriteString(";4")
		}
	}
	if style.attrs&AttrBlink != 0 {
		out.WriteString(";5")
	}
	if style.inverse {
		out.WriteString(";7")
	}
	if style.attrs&AttrStrikethrough != 0 {
		out.WriteString(";9")
	}
	var scratch [16]byte
	writeOutputColor(out, &scratch, style.foreground, 38, 30, 90)
	writeOutputColor(out, &scratch, style.background, 48, 40, 100)
	writeUnderlineColor(out, &scratch, style.underlineColor)
	out.WriteByte('m')
}

func writeOutputColor(out *bytes.Buffer, scratch *[16]byte, color outputColor, extended, normal, bright int) {
	switch color.kind {
	case outputColorANSI16:
		code := normal + color.index
		if color.index >= 8 {
			code = bright + color.index - 8
		}
		writeSGRParam(out, scratch, code)
	case outputColorANSI256:
		writeExtendedColor(out, scratch, extended, 5, color.index)
	case outputColorRGB:
		writeExtendedRGB(out, scratch, extended, color.rgb)
	}
}

func writeUnderlineColor(out *bytes.Buffer, scratch *[16]byte, color outputColor) {
	switch color.kind {
	case outputColorANSI256:
		writeExtendedColor(out, scratch, 58, 5, color.index)
	case outputColorRGB:
		writeExtendedRGB(out, scratch, 58, color.rgb)
	}
}

func writeSGRParam(out *bytes.Buffer, scratch *[16]byte, value int) {
	out.WriteByte(';')
	out.Write(strconv.AppendInt(scratch[:0], int64(value), 10))
}

func writeExtendedColor(out *bytes.Buffer, scratch *[16]byte, selector, mode, value int) {
	writeSGRParam(out, scratch, selector)
	writeSGRParam(out, scratch, mode)
	writeSGRParam(out, scratch, value)
}

func writeExtendedRGB(out *bytes.Buffer, scratch *[16]byte, selector int, rgb RGB) {
	writeSGRParam(out, scratch, selector)
	writeSGRParam(out, scratch, 2)
	writeSGRParam(out, scratch, int(rgb.R))
	writeSGRParam(out, scratch, int(rgb.G))
	writeSGRParam(out, scratch, int(rgb.B))
}
