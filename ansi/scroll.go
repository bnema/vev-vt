package ansi

import (
	"bytes"
	"strconv"
)

func findSafeScroll(frame CellSource, damage []Damage) (Damage, bool) {
	for _, d := range damage {
		if (d.Kind == DamageScrollUp || d.Kind == DamageScrollDown) && isSafeScroll(frame, d) {
			return d, true
		}
	}
	return Damage{}, false
}

func isSafeScroll(frame CellSource, d Damage) bool {
	return d.X == 0 && d.Width == frame.Columns() && d.Y >= 0 && d.Y <= frame.Rows() && d.Height > 0 && d.Height <= frame.Rows()-d.Y && d.Count > 0 && d.Count < d.Height
}

func emitScroll(out *bytes.Buffer, d Damage) {
	out.WriteString("\x1b[0m")
	// \x1b[%d;%dr — set scroll region
	out.WriteString("\x1b[")
	var b [16]byte
	n := strconv.AppendInt(b[:0], int64(d.Y+1), 10)
	out.Write(n)
	out.WriteByte(';')
	n = strconv.AppendInt(b[:0], int64(d.Y+d.Height), 10)
	out.Write(n)
	out.WriteByte('r')
	// SD scrolls down without moving content outside the chosen region.
	if d.Kind == DamageScrollDown {
		out.WriteString("\x1b[")
		n = strconv.AppendInt(b[:0], int64(d.Count), 10)
		out.Write(n)
		out.WriteByte('T')
		out.WriteString("\x1b[r")
		return
	}
	// Position cursor at bottom of scroll region.
	writeCursor(out, d.Y+d.Height-1, 0)
	if d.Count == 1 {
		out.WriteByte('\n')
	} else {
		out.WriteString("\x1b[")
		n = strconv.AppendInt(b[:0], int64(d.Count), 10)
		out.Write(n)
		out.WriteByte('S')
	}
	out.WriteString("\x1b[r")
}

func canApplyScrollAgainst(frame CellSource, scroll Damage, damage []Damage, committed Frame) bool {
	// Scrolling fills exposed rows with default-style blanks. Every target
	// cell that differs from that fill must be repainted, in either direction.
	exposed := scroll.Y + scroll.Height - scroll.Count
	if scroll.Kind == DamageScrollDown {
		exposed = scroll.Y
	}
	blank := BlankCell()
	for y := exposed; y < exposed+scroll.Count; y++ {
		for x := range scroll.Width {
			column := scroll.X + x
			if !damageCoversCell(damage, column, y) && !frame.Cell(column, y).Equal(blank) {
				return false
			}
		}
	}
	switch frame := frame.(type) {
	case Frame:
		return canApplyDenseScrollAgainst(frame, scroll, damage, committed)
	case *Frame:
		return canApplyDenseScrollAgainst(*frame, scroll, damage, committed)
	}
	start, end, offset := scrollRetainedRows(scroll)
	for y := start; y < end; y++ {
		for x := range scroll.Width {
			column := scroll.X + x
			committedCell, frameCell := committed.Cell(column, y+offset), frame.Cell(column, y)
			if committedCell == frameCell || committedCell.Equal(frameCell) {
				continue
			}
			if !damageCoversCell(damage, column, y) {
				return false
			}
		}
	}
	return true
}

func canApplyDenseScrollAgainst(frame Frame, scroll Damage, damage []Damage, committed Frame) bool {
	start, end, offset := scrollRetainedRows(scroll)
	for y := start; y < end; y++ {
		for x := range scroll.Width {
			column := scroll.X + x
			committedCell, frameCell := committed.At(column, y+offset), frame.At(column, y)
			if committedCell == frameCell || committedCell.Equal(frameCell) {
				continue
			}
			if !damageCoversCell(damage, column, y) {
				return false
			}
		}
	}
	return true
}

func scrollRetainedRows(scroll Damage) (start, end, offset int) {
	if scroll.Kind == DamageScrollDown {
		return scroll.Y + scroll.Count, scroll.Y + scroll.Height, -scroll.Count
	}
	return scroll.Y, scroll.Y + scroll.Height - scroll.Count, scroll.Count
}

func damageCoversCell(damage []Damage, x, y int) bool {
	for _, d := range damage {
		if (d.Kind == DamageText || d.Kind == DamageClear) && x >= d.X && x < d.X+d.Width && y >= d.Y && y < d.Y+d.Height {
			return true
		}
	}
	return false
}
