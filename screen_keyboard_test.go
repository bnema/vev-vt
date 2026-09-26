package vt

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScreenKittyKeyboardFlags(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "default", input: "", want: 0},
		{name: "push", input: "\x1b[>1u", want: 1},
		{name: "push without flags", input: "\x1b[>u", want: 0},
		{name: "push masks unknown bits", input: "\x1b[>255u", want: 31},
		{name: "push twice", input: "\x1b[>1u\x1b[>11u", want: 11},
		{name: "pop default one", input: "\x1b[>1u\x1b[>11u\x1b[<u", want: 1},
		{name: "pop n", input: "\x1b[>1u\x1b[>3u\x1b[>11u\x1b[<2u", want: 1},
		{name: "pop past empty", input: "\x1b[>1u\x1b[<5u", want: 0},
		{name: "pop empty", input: "\x1b[<u", want: 0},
		{name: "set replace on empty", input: "\x1b[=5u", want: 5},
		{name: "set replace", input: "\x1b[>1u\x1b[=8;1u", want: 8},
		{name: "set or", input: "\x1b[>1u\x1b[=8;2u", want: 9},
		{name: "set clear", input: "\x1b[>11u\x1b[=2;3u", want: 9},
		{name: "set unknown mode ignored", input: "\x1b[>1u\x1b[=8;4u", want: 1},
		{name: "set replaces top only", input: "\x1b[>1u\x1b[>2u\x1b[=4u\x1b[<u", want: 1},
		{name: "malformed flags are zero", input: "\x1b[>xu", want: 0},
		{name: "plain CSI u restores cursor only", input: "\x1b[>3u\x1b[u", want: 3},
		{name: "RIS resets", input: "\x1b[>3u\x1bc", want: 0},
		{name: "alternate screen has its own stack", input: "\x1b[>1u\x1b[?1049h", want: 0},
		{name: "alternate screen push", input: "\x1b[>1u\x1b[?1049h\x1b[>15u", want: 15},
		{name: "leaving alternate restores main stack", input: "\x1b[>1u\x1b[?1049h\x1b[>15u\x1b[?1049l", want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScreen(10, 3)
			s.Write([]byte(tt.input))
			require.Equal(t, tt.want, s.KittyKeyboardFlags())
			require.Equal(t, tt.want, s.Snapshot().Modes().KittyKeyboard)
		})
	}
}

func TestScreenKittyKeyboardStackIsBounded(t *testing.T) {
	s := NewScreen(10, 3)
	s.Write([]byte("\x1b[>1u"))
	s.Write([]byte(strings.Repeat("\x1b[>2u", maxKittyKeyboardStack)))
	require.Len(t, s.kittyKeyboard, maxKittyKeyboardStack)
	s.Write([]byte(strings.Repeat("\x1b[<u", maxKittyKeyboardStack-1)))
	require.Equal(t, 2, s.KittyKeyboardFlags(), "oldest entry was evicted")
	s.Write([]byte("\x1b[<u"))
	require.Equal(t, 0, s.KittyKeyboardFlags())
}

func TestScreenKittyKeyboardQuery(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "empty", input: "\x1b[?u", want: []string{"\x1b[?0u"}},
		{name: "after push", input: "\x1b[>11u\x1b[?u", want: []string{"\x1b[?11u"}},
		{name: "with params is ignored", input: "\x1b[?1u", want: nil},
		{name: "split across writes", input: "\x1b[>3u\x1b[?", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScreen(10, 3)
			var got []string
			s.OnResponse = func(b []byte) { got = append(got, string(b)) }
			s.Write([]byte(tt.input))
			require.Equal(t, tt.want, got)
		})
	}
}
