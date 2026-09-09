package html

import (
	"fmt"
	"testing"

	"github.com/bnema/vev-vt/core"
)

func BenchmarkRenderer(b *testing.B) {
	for _, size := range []struct{ width, height int }{{80, 24}, {120, 40}, {240, 80}} {
		frame := core.NewFrame(size.width, size.height)
		for y := range size.height {
			for x := range size.width {
				frame.Set(x, y, core.Cell{Rune: rune('A' + (x+y)%26), Style: core.DefaultStyle()})
			}
		}
		b.Run(fmt.Sprintf("snapshot-%dx%d", size.width, size.height), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				renderer, err := New(Options{})
				if err != nil {
					b.Fatal(err)
				}
				prepared, err := renderer.Prepare(frame, nil, true, Cursor{})
				if err != nil {
					b.Fatal(err)
				}
				if err := prepared.Commit(); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("one-row-%dx%d", size.width, size.height), func(b *testing.B) {
			renderer, err := New(Options{})
			if err != nil {
				b.Fatal(err)
			}
			first, err := renderer.Prepare(frame, nil, false, Cursor{})
			if err != nil {
				b.Fatal(err)
			}
			if err := first.Commit(); err != nil {
				b.Fatal(err)
			}
			left := frame.Clone()
			right := frame.Clone()
			right.Set(0, size.height/2, core.Cell{Rune: 'Z', Style: core.DefaultStyle()})
			b.ReportAllocs()
			b.ResetTimer()
			for index := range b.N {
				candidate := left
				if index%2 == 0 {
					candidate = right
				}
				prepared, err := renderer.Prepare(candidate, nil, false, Cursor{})
				if err != nil {
					b.Fatal(err)
				}
				if err := prepared.Commit(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
