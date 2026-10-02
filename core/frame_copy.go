package core

import "maps"

// CopyFrom makes f an independent structural copy of src, like Replace, but
// reuses f's existing page storage (cells, row descriptors, style and payload
// tables and dictionaries) when f already owns a page. It therefore performs no
// allocation once f's page has grown to src's size.
//
// Page-local style and payload IDs and physical row rotation are copied
// verbatim, exactly as Clone does. Every Frame value sharing f's page observes
// the overwrite. An invalid or empty source leaves f unchanged. Copying a frame
// onto itself only normalizes the dimensions.
func (f *Frame) CopyFrom(src Frame) {
	if f == nil {
		return
	}
	if err := src.validateStorage(); err != nil || src.Width <= 0 || src.Height <= 0 {
		return
	}
	if f.page == nil {
		*f = src.Clone()
		return
	}
	f.Width, f.Height = src.Width, src.Height
	dst, from := f.page, src.page
	if dst == from {
		return
	}
	dst.cells = append(dst.cells[:0], from.cells...)
	dst.rows = append(dst.rows[:0], from.rows...)
	dst.styles = append(dst.styles[:0], from.styles...)
	dst.freeStyles = append(dst.freeStyles[:0], from.freeStyles...)
	dst.styleCount = from.styleCount
	// The cache refers to a slot in the style table copied above, so copying it
	// keeps it valid. Leaving the destination's old cache would point at a slot
	// that may now hold a different style.
	dst.styleCache, dst.styleCacheID, dst.styleCacheOK = from.styleCache, from.styleCacheID, from.styleCacheOK
	if dst.styleIndex == nil {
		dst.styleIndex = make(map[Style]uint32, len(from.styleIndex))
	} else {
		clear(dst.styleIndex)
	}
	maps.Copy(dst.styleIndex, from.styleIndex)
	dst.payloads = append(dst.payloads[:0], from.payloads...)
	dst.freePayloads = append(dst.freePayloads[:0], from.freePayloads...)
	dst.payloadBytes = from.payloadBytes
	if len(from.payloadIndex) == 0 {
		clear(dst.payloadIndex)
	} else {
		if dst.payloadIndex == nil {
			dst.payloadIndex = make(map[CellPayload]uint32, len(from.payloadIndex))
		} else {
			clear(dst.payloadIndex)
		}
		maps.Copy(dst.payloadIndex, from.payloadIndex)
	}
}

// RowsEqualAt reports whether logical row ya of a and logical row yb of b hold
// semantically equal cells (the same comparison as Cell.Equal for every
// column). It compares compact stored cells directly and resolves page-local
// style and payload IDs only when they are not both the default, so it does
// not allocate and does not require the two frames to share ID tables. Frames
// of different width, an out-of-range row, or inconsistent storage compare
// unequal.
func RowsEqualAt(a Frame, ya int, b Frame, yb int) bool {
	if a.Width != b.Width || a.Width <= 0 || a.page == nil || b.page == nil ||
		ya < 0 || ya >= a.Height || yb < 0 || yb >= b.Height ||
		ya >= len(a.page.rows) || yb >= len(b.page.rows) {
		return false
	}
	width := a.Width
	offA, offB := int(a.page.rows[ya]), int(b.page.rows[yb])
	if offA+width > len(a.page.cells) || offB+width > len(b.page.cells) {
		return false
	}
	rowA := a.page.cells[offA : offA+width]
	rowB := b.page.cells[offB : offB+width]

	// Remember the last style ID pair that compared equal so runs of one style
	// cost a single table lookup.
	var lastStyleA, lastStyleB uint32
	for x := range rowA {
		ca, cb := rowA[x], rowB[x]
		if ca.rune != cb.rune || ca.flags != cb.flags {
			return false
		}
		if ca.styleID != 0 || cb.styleID != 0 {
			if ca.styleID != lastStyleA || cb.styleID != lastStyleB {
				if !a.storedStylesEqual(b, ca.styleID, cb.styleID) {
					return false
				}
				lastStyleA, lastStyleB = ca.styleID, cb.styleID
			}
		}
		if ca.payloadID != 0 || cb.payloadID != 0 {
			if ca.payloadID == 0 || cb.payloadID == 0 ||
				int(ca.payloadID) > len(a.page.payloads) || int(cb.payloadID) > len(b.page.payloads) ||
				a.payload(ca.payloadID) != b.payload(cb.payloadID) {
				return false
			}
		}
	}
	return true
}

func (f Frame) storedStylesEqual(other Frame, id, otherID uint32) bool {
	if int(id) >= len(f.page.styles) || int(otherID) >= len(other.page.styles) {
		return false
	}
	sa, sb := f.page.styles[id].style, other.page.styles[otherID].style
	return sa == sb || sa.Equal(sb)
}
