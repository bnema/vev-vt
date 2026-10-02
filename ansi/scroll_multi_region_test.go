package ansi_test

import (
	"testing"

	vt "github.com/bnema/vev-vt"
	"github.com/bnema/vev-vt/ansi"
	core "github.com/bnema/vev-vt/core"
)

// Two scroll regions in one damage batch cannot be expressed as one scroll
// plus text spans; the renderer must still reproduce the source exactly.
func TestTwoScrollRegionsInOneBatchReplayExactly(t *testing.T) {
	for _, sourceKind := range []string{"screen", "frame", "pointer"} {
		t.Run(sourceKind, func(t *testing.T) {
			s := vt.NewScreen(10, 8)
			s.Write([]byte("\x1b[1;1Ha0\r\na1\r\na2\r\na3\r\nb4\r\nb5\r\nb6\r\nb7"))
			r := ansi.New(ansi.Capabilities{})
			replay := vt.NewScreen(10, 8)
			draw := func() {
				var source core.CellSource = s
				if sourceKind != "screen" {
					frame := core.NewFrame(10, 8)
					for y := range 8 {
						for x := range 10 {
							frame.Set(x, y, s.Cell(x, y))
						}
					}
					source = frame
					if sourceKind == "pointer" {
						source = &frame
					}
				}
				prepared, err := r.Prepare(source, s.Damage(), false)
				if err != nil {
					t.Fatal(err)
				}
				prepared.Commit()
				replay.Write(prepared.Bytes())
				s.ClearDamage()
			}
			draw()
			s.Write([]byte("\x1b[1;4r\x1b[4;1H\nX\x1b[5;8r\x1b[8;1H\n\nY\x1b[r"))
			draw()
			for y := range 8 {
				for x := range 10 {
					if want, got := s.Cell(x, y), replay.Cell(x, y); want.Rune != got.Rune {
						t.Fatalf("cell (%d,%d) = %q, want %q", x, y, got.Rune, want.Rune)
					}
				}
			}
		})
	}
}
