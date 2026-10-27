package core

import (
	"fmt"
	"testing"

	"github.com/rivo/uniseg"
)

func TestRuneWidthBMPEmojiPresentation(t *testing.T) {
	// Unicode 15.0 Emoji_Presentation=Yes BMP runes. Check the entire
	// surrounding symbols range against uniseg so narrow neighbors stay narrow.
	for r := rune(0x2300); r <= 0x2BFF; r++ {
		t.Run(fmt.Sprintf("U+%04X", r), func(t *testing.T) {
			want := uniseg.StringWidth(string(r))
			if got := RuneWidth(r); got != want {
				t.Errorf("RuneWidth(%U) = %d, want %d (uniseg)", r, got, want)
			}
		})
	}
}

func TestRuneWidthEmojiRegression(t *testing.T) {
	for _, tt := range []struct {
		r    rune
		want int
	}{
		{'✅', 2}, {'❌', 2}, {'✊', 2}, {'⭐', 2},
		{'✓', 1}, {'✗', 1}, {'❤', 1}, {'\uFE0F', 0},
	} {
		t.Run(fmt.Sprintf("U+%04X", tt.r), func(t *testing.T) {
			if got := RuneWidth(tt.r); got != tt.want {
				t.Errorf("RuneWidth(%U) = %d, want %d", tt.r, got, tt.want)
			}
		})
	}
}
