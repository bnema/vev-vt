package vt

import (
	"strings"
	"testing"
)

func TestCharsetsAndRepeat(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "G0 DEC special graphics draws lines", input: "\x1b(0lqqk\x1b(Bq", want: "┌──┐q"},
		{name: "G1 via SO and SI", input: "\x1b)0q\x0eqx\x0fq", want: "q─│q"},
		{name: "SO without G1 designation stays ASCII", input: "\x0eq\x0f", want: "q"},
		{name: "chars outside the graphics range pass through", input: "\x1b(0Aa~", want: "A▒·"},
		{name: "DECSC and DECRC restore charsets", input: "\x1b(0\x1b7\x1b(B\x1b8q", want: "─"},
		{name: "DECSTR resets charsets", input: "\x1b(0q\x1b[!pq", want: "─q"},
		{name: "RIS resets charsets", input: "\x1b(0\x1bcq", want: "q"},
		{name: "REP repeats the last graphic char", input: "\x1b(0q\x1b[4b", want: "─────"},
		{name: "REP defaults to one", input: "a\x1b[b", want: "aa"},
		{name: "REP without a prior char is ignored", input: "\x1b[3bx", want: "x"},
		{name: "REP after a control is ignored", input: "ab\r\x1b[2b", want: "ab"},
		{name: "REP after another sequence is ignored", input: "a\x1b[1m\x1b[2b", want: "a"},
		{name: "consecutive REP repeats once", input: "a\x1b[b\x1b[b", want: "aa"},
		{name: "DECSC and DECRC restore SO invocation", input: "\x1b)0\x0e\x1b7\x0f\x1b8q", want: "─"},
		{name: "leaving the alternate screen restores charsets", input: "\x1b(0\x1b[?1049h\x1b(B\x1b[?1049lq", want: "─"},
		{name: "DECSTR saved cursor restores home", input: "\x1b[1;5Hx\x1b[!p\x1b8y", want: "y   x"},
		{name: "partial charset escape across writes", input: "\x1b(", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScreen(10, 3)
			s.Write([]byte(tt.input))
			if got := strings.TrimRight(lineText(s, 0), " "); got != tt.want {
				t.Fatalf("line = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCharsetDesignationSplitAcrossWrites(t *testing.T) {
	s := NewScreen(10, 1)
	s.Write([]byte("\x1b("))
	s.Write([]byte("0q"))
	if got := strings.TrimRight(lineText(s, 0), " "); got != "─" {
		t.Fatalf("line = %q, want %q", got, "─")
	}
}

func TestSoftResetCursorAppearance(t *testing.T) {
	s := NewScreen(4, 1)
	s.Write([]byte("\x1b[6 q\x1b[?25l\x1b[!p"))
	if s.cursorStyleSet || s.cursorStyle != 0 || !s.cursorVisible {
		t.Fatalf("cursor style=%d set=%v visible=%v, want defaults", s.cursorStyle, s.cursorStyleSet, s.cursorVisible)
	}
}

func TestRepeatSplitAcrossWrites(t *testing.T) {
	s := NewScreen(10, 1)
	s.Write([]byte("a\x1b["))
	s.Write([]byte("2b"))
	if got := strings.TrimRight(lineText(s, 0), " "); got != "aaa" {
		t.Fatalf("line = %q, want %q", got, "aaa")
	}
}

func TestRepeatIsBounded(t *testing.T) {
	s := NewScreen(4, 2)
	s.Write([]byte("x\x1b[999999999b"))
	if got := lineText(s, 0); got != "xxxx" {
		t.Fatalf("line = %q, want %q", got, "xxxx")
	}
}
