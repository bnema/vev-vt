package ansi_test

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	vt "github.com/bnema/vev-vt"
	"github.com/bnema/vev-vt/ansi"
	core "github.com/bnema/vev-vt/core"
)

type hiddenCellSource struct{ core.CellSource }

// Random VT scroll workloads rendered with real screen damage must replay to
// the exact source grid for every CellSource shape the renderer dispatches on.
func TestRandomScrollWorkloadsReplayExactly(t *testing.T) {
	seeds := 400
	if testing.Short() {
		seeds = 60
	}
	for _, kind := range []string{"screen", "frame", "pointer", "hidden"} {
		t.Run(kind, func(t *testing.T) {
			for seed := range seeds {
				runReplayWorkload(t, kind, uint64(seed))
			}
		})
	}
}

func runReplayWorkload(t *testing.T, kind string, seed uint64) {
	t.Helper()
	r := rand.New(rand.NewPCG(seed, 0x5c4011))
	cols, rows := 4+r.IntN(14), 3+r.IntN(10)
	s := vt.NewScreen(cols, rows)
	renderer := ansi.New(ansi.Capabilities{})
	replay := vt.NewScreen(cols, rows)
	mirror := core.NewFrame(cols, rows)
	for step := range 30 {
		input := randomScrollInput(r, cols, rows)
		s.Write(input)
		var source core.CellSource = s
		if kind != "screen" {
			for y := range rows {
				for x := range cols {
					mirror.Set(x, y, s.Cell(x, y))
				}
			}
			switch kind {
			case "frame":
				source = mirror
			case "pointer":
				source = &mirror
			default:
				source = hiddenCellSource{mirror}
			}
		}
		prepared, err := renderer.Prepare(source, s.Damage(), false)
		if err != nil {
			t.Fatal(err)
		}
		prepared.Commit()
		replay.Write(prepared.Bytes())
		s.ClearDamage()
		for y := range rows {
			for x := range cols {
				want, got := s.Cell(x, y), replay.Cell(x, y)
				if want.Rune != got.Rune || want.Continuation != got.Continuation || !want.Style.Equal(got.Style) {
					t.Fatalf("seed %d step %d input %q: cell (%d,%d) = %+v, want %+v", seed, step, input, x, y, got, want)
				}
			}
		}
	}
}

func randomScrollInput(r *rand.Rand, cols, rows int) []byte {
	var b strings.Builder
	for range 1 + r.IntN(12) {
		switch r.IntN(14) {
		case 0:
			top := 1 + r.IntN(rows)
			fmt.Fprintf(&b, "\x1b[%d;%dr", top, top+r.IntN(rows-top+1))
		case 1:
			b.WriteString("\x1b[r")
		case 2:
			b.WriteString("\x1bD")
		case 3:
			b.WriteString("\x1bM")
		case 4:
			fmt.Fprintf(&b, "\x1b[%dS", 1+r.IntN(3))
		case 5:
			fmt.Fprintf(&b, "\x1b[%dT", 1+r.IntN(3))
		case 6:
			fmt.Fprintf(&b, "\x1b[%dL", 1+r.IntN(3))
		case 7:
			fmt.Fprintf(&b, "\x1b[%dM", 1+r.IntN(3))
		case 8:
			b.WriteString("\r\n")
		case 9:
			b.WriteString([]string{"\x1b[K", "\x1b[1K", "\x1b[2K", "\x1b[J"}[r.IntN(4)])
		case 10:
			fmt.Fprintf(&b, "\x1b[%dm", []int{0, 1, 7, 31, 42}[r.IntN(5)])
		case 11:
			fmt.Fprintf(&b, "\x1b[%d;%dH", 1+r.IntN(rows), 1+r.IntN(cols))
		case 12:
			b.WriteString("界")
		default:
			for range 1 + r.IntN(cols) {
				b.WriteByte(byte('a' + r.IntN(26)))
			}
		}
	}
	return []byte(b.String())
}
