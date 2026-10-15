package vt

// decSpecialGraphics maps 0x5f..0x7e to the DEC Special Graphics set
// (designated by ESC ( 0 or ESC ) 0), used by ncurses for line drawing.
var decSpecialGraphics = [...]rune{
	' ', '◆', '▒', '␉', '␌', '␍', '␊', '°', '±', '␤', '␋', '┘', '┐', '┌', '└', '┼',
	'⎺', '⎻', '─', '⎼', '⎽', '├', '┤', '┴', '┬', '│', '≤', '≥', 'π', '≠', '£', '·',
}

// maxRepeat bounds REP to one full screen: repeating further only rewrites
// cells and scrolls, so a hostile count cannot stall the parser.
func maxRepeat(width, height int) int {
	return max(width*height, 1)
}

// translateCharset maps r through the character set currently invoked into GL.
func (s *Screen) translateCharset(r rune) rune {
	g := 0
	if s.shiftOut {
		g = 1
	}
	if s.charsets[g] == '0' && r >= 0x5f && r <= 0x7e {
		return decSpecialGraphics[r-0x5f]
	}
	return r
}
