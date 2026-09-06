package vt

import (
	"bytes"
	"testing"

	renderer "github.com/bnema/vev-vt/core"
)

func TestScreenAutoWrapModeRightMargin(t *testing.T) {
	tests := []struct {
		name        string
		width       int
		height      int
		input       string
		wantRows    []string
		wantRow     int
		wantCol     int
		wantHistory int
	}{
		{
			name:  "successive writes overwrite right margin",
			width: 4, height: 2, input: "\x1b[?7labcde",
			wantRows: []string{"abce", "    "}, wantRow: 0, wantCol: 3,
		},
		{
			name:  "bottom row does not scroll",
			width: 3, height: 2, input: "top\r\n\x1b[?7labcd",
			wantRows: []string{"top", "abd"}, wantRow: 1, wantCol: 2,
		},
		{
			name:  "width one overwrites",
			width: 1, height: 2, input: "\x1b[?7lab",
			wantRows: []string{"b", " "}, wantRow: 0, wantCol: 0,
		},
		{
			name:  "vev raw-mode initialization disables wrapping",
			width: 4, height: 2, input: "\x1b[?7lstatus",
			wantRows: []string{"stas", "    "}, wantRow: 0, wantCol: 3,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := NewScreenWithHistory(test.width, test.height, HistoryConfig{MaxRows: 10})
			s.Write([]byte(test.input))
			for row, want := range test.wantRows {
				if got := screenRowText(s, row); got != want {
					t.Fatalf("row %d = %q, want %q", row, got, want)
				}
			}
			if s.Row != test.wantRow || s.Col != test.wantCol {
				t.Fatalf("cursor = (%d,%d), want (%d,%d)", s.Row, s.Col, test.wantRow, test.wantCol)
			}
			if got := s.History().Len(); got != test.wantHistory {
				t.Fatalf("history length = %d, want %d", got, test.wantHistory)
			}
			if s.buffer.boundaries[0].Soft {
				t.Fatal("disabled autowrap created a soft wrap")
			}
		})
	}
}

func TestScreenAutoWrapWideRuneAtRightMargin(t *testing.T) {
	s := NewScreen(4, 2)
	s.Write([]byte("abc\x1b[?7l界"))

	if got := s.Cell(3, 0); got.Rune != '\uFFFD' || got.Continuation {
		t.Fatalf("right-margin cell = %#v, want narrow replacement", got)
	}
	for x := 0; x < s.Columns(); x++ {
		if s.Cell(x, 0).Continuation && (x == 0 || renderer.RuneWidth(s.Cell(x-1, 0).Rune) != 2) {
			t.Fatalf("orphaned continuation at column %d", x)
		}
	}
	if s.Row != 0 || s.Col != 3 {
		t.Fatalf("cursor = (%d,%d), want (0,3)", s.Row, s.Col)
	}
}

func TestScreenAutoWrapModeTransitionsAndResize(t *testing.T) {
	s := NewScreen(4, 2)
	s.Write([]byte("abcd")) // Leave a deferred wrap pending.
	s.Write([]byte("\x1b[?7le"))
	if got := screenRowText(s, 0); got != "abce" {
		t.Fatalf("row after disabling pending wrap = %q, want %q", got, "abce")
	}

	s.Resize(3, 2)
	if s.Row != 0 || s.Col != 2 {
		t.Fatalf("cursor after resize = (%d,%d), want (0,2)", s.Row, s.Col)
	}
	s.Write([]byte("f\x1b[?7hgh"))
	if s.Row != 1 || s.Col != 1 {
		t.Fatalf("cursor after re-enabling wrap = (%d,%d), want (1,1)", s.Row, s.Col)
	}
	if got := screenRowText(s, 1); got != "h  " {
		t.Fatalf("wrapped row = %q, want %q", got, "h  ")
	}
}

func TestScreenAutoWrapSavedAndReset(t *testing.T) {
	s := NewScreen(4, 2)
	s.Write([]byte("\x1b[?7l\x1b7\x1b[?7h\x1b8"))
	if s.AutoWrapMode() {
		t.Fatal("ESC 8 did not restore disabled autowrap mode")
	}

	s.Write([]byte("\x1bc"))
	if !s.AutoWrapMode() {
		t.Fatal("RIS did not restore enabled autowrap default")
	}
}

func TestScreenApplicationCursorMode(t *testing.T) {
	s := NewScreen(4, 2)
	if s.ApplicationCursorMode() {
		t.Fatal("application cursor mode enabled by default")
	}
	s.Write([]byte("\x1b[?"))
	s.Write([]byte("1h"))
	if !s.ApplicationCursorMode() || !s.Snapshot().Modes().ApplicationCursor {
		t.Fatal("split DECCKM enable was not tracked in screen and snapshot")
	}
	s.Write([]byte("\x1b[?1l"))
	if s.ApplicationCursorMode() {
		t.Fatal("DECCKM disable was not tracked")
	}
	s.Write([]byte("\x1b[?1h\x1bc"))
	if s.ApplicationCursorMode() {
		t.Fatal("RIS did not reset DECCKM")
	}
}

func TestScreenCursorAndAutoWrapModeQueries(t *testing.T) {
	s := NewScreen(4, 2)
	var got bytes.Buffer
	s.OnResponse = func(response []byte) { got.Write(response) }
	s.Write([]byte("\x1b[?1$p\x1b[?7$p\x1b[?1h\x1b[?7l\x1b[?1$p\x1b[?7$p"))
	want := "\x1b[?1;2$y\x1b[?7;1$y\x1b[?1;1$y\x1b[?7;2$y"
	if got.String() != want {
		t.Fatalf("mode reports = %q, want %q", got.String(), want)
	}
}

func screenRowText(s *Screen, row int) string {
	runes := make([]rune, s.Columns())
	for col := range s.Columns() {
		r := s.Cell(col, row).Rune
		if r == 0 {
			r = ' '
		}
		runes[col] = r
	}
	return string(runes)
}
