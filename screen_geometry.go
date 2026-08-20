package vt

// Geometry returns the screen's last frontend-supplied geometry.
func (s *Screen) Geometry() Geometry {
	if s == nil {
		return Geometry{}
	}
	return s.geometry
}

// SetGeometry updates the frontend-supplied cell and optional pixel geometry.
// Cell changes retain the existing Resize behavior; pixel-only changes do not
// touch the text frame or damage state.
func (s *Screen) SetGeometry(geometry Geometry) {
	if s == nil || !geometry.Valid() {
		return
	}
	cellsChanged := s.geometry.Cols != geometry.Cols || s.geometry.Rows != geometry.Rows
	s.geometry = geometry
	if cellsChanged {
		s.Resize(geometry.Cols, geometry.Rows)
	}
}

// reportWindowSize answers the CSI 14 t and CSI 16 t pixel-geometry queries.
// Unknown pixel geometry is intentionally not fabricated.
func (s *Screen) reportWindowSize(parts []int) {
	if s == nil || len(parts) == 0 || !s.geometry.PixelsKnown() {
		return
	}
	kind := parts[0]
	var rows, cols int
	switch kind {
	case 14:
		rows, cols = s.geometry.PixelHeight, s.geometry.PixelWidth
	case 16:
		if s.geometry.Rows <= 0 || s.geometry.Cols <= 0 {
			return
		}
		rows, cols = s.geometry.PixelHeight/s.geometry.Rows, s.geometry.PixelWidth/s.geometry.Cols
	default:
		return
	}
	s.respond([]byte("\x1b[" + itoa(kind-10) + ";" + itoa(rows) + ";" + itoa(cols) + "t"))
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[i:])
}
