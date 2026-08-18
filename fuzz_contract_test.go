package vt_test

import (
	"testing"

	vt "github.com/bnema/vev-vt"
	"github.com/bnema/vev-vt/ansi"
	core "github.com/bnema/vev-vt/core"
)

func FuzzVTInputDoesNotPanic(f *testing.F) {
	f.Add([]byte("hello\r\n"))
	f.Add([]byte("\x1b[2J\x1b[?1049h界\x1b[?1049l"))
	f.Add([]byte("\x1b]52;c;SGVsbG8=\a"))
	f.Add([]byte{0x1b, '[', '3', '1'})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			data = data[:4096]
		}
		screen := vt.NewScreen(80, 24)
		screen.Write(data)
		_ = screen.Snapshot()
	})
}

func FuzzANSIRenderingDoesNotPanic(f *testing.F) {
	f.Add([]byte("hello"))
	f.Add([]byte("界🙂"))
	f.Add([]byte{0, 1, 2, 0x1b, 0x7f, 0xff})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			data = data[:4096]
		}
		frame := core.NewFrame(16, 2)
		for index, value := range data {
			cell := core.Cell{Rune: rune(value), Style: core.DefaultStyle()}
			frame.Set(index%frame.Width, (index/frame.Width)%frame.Height, cell)
		}
		if _, err := ansi.New(ansi.Capabilities{}).Draw(frame, []core.Damage{core.FullRedraw()}); err != nil {
			t.Fatalf("Draw: %v", err)
		}
	})
}
