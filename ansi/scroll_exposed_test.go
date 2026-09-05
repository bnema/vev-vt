package ansi_test

import (
	"fmt"
	"testing"

	vt "github.com/bnema/vev-vt"
	"github.com/bnema/vev-vt/ansi"
)

// An embedded source exercises the generic path rather than Frame dispatch.
type scrollCellSource struct{ vt.CellSource }

func TestScrollExposedCellsRequireBlankOrRepaint(t *testing.T) {
	for _, down := range []bool{false, true} {
		for _, sourceKind := range []string{"frame", "pointer", "generic"} {
			for _, tc := range []struct {
				name     string
				cell     vt.Cell
				coverage int
				kind     vt.DamageKind
				fallback bool
			}{
				{"blank", vt.BlankCell(), 0, vt.DamageText, false},
				{"uncovered text", vt.Cell{Rune: 'X', Style: vt.DefaultStyle()}, 0, vt.DamageText, true},
				{"uncovered styled blank", vt.Cell{Rune: ' ', Style: vt.Style{Bold: true}}, 0, vt.DamageText, true},
				{"partial coverage", vt.Cell{Rune: 'X', Style: vt.DefaultStyle()}, 1, vt.DamageText, true},
				{"text coverage", vt.Cell{Rune: 'X', Style: vt.DefaultStyle()}, 2, vt.DamageText, false},
				{"clear coverage", vt.Cell{Rune: 'X', Style: vt.DefaultStyle()}, 2, vt.DamageClear, false},
			} {
				t.Run(fmt.Sprintf("down=%v/%s/%s", down, sourceKind, tc.name), func(t *testing.T) {
					committed := vt.NewFrame(12, 10)
					for y := range committed.Height {
						committed.FillRow(y, 0, committed.Width, vt.Cell{Rune: rune('a' + y), Style: vt.DefaultStyle()})
					}
					frame := committed.Clone()
					kind, exposed := vt.DamageScrollUp, 6
					if down {
						kind, exposed = vt.DamageScrollDown, 2
						frame.ScrollDown(2, 7, 2)
					} else {
						frame.ScrollUp(2, 7, 2)
					}
					for y := exposed; y < exposed+2; y++ {
						frame.Set(0, y, tc.cell)
					}
					damage := []vt.Damage{{Kind: kind, Y: 2, Width: 12, Height: 6, Count: 2}}
					if tc.coverage > 0 {
						damage = append(damage, vt.Damage{Kind: tc.kind, Y: exposed, Width: 12, Height: tc.coverage})
					}
					var source vt.CellSource = frame
					switch sourceKind {
					case "pointer":
						source = &frame
					case "generic":
						source = scrollCellSource{frame}
					}
					candidate, err := ansi.PlanDelta(source, damage, committed, false)
					if err != nil {
						t.Fatal(err)
					}
					if candidate.Plan.Snapshot != tc.fallback {
						t.Fatalf("plan = %+v, want fallback %v", candidate.Plan, tc.fallback)
					}
					r := ansi.New(ansi.Capabilities{})
					terminal := vt.NewScreen(12, 10)
					initial, err := r.Draw(committed, nil)
					if err != nil {
						t.Fatal(err)
					}
					terminal.Write(initial)
					data, err := r.Draw(source, damage)
					if err != nil {
						t.Fatal(err)
					}
					terminal.Write(data)
					for y := range frame.Height {
						for x := range frame.Width {
							if !frame.Cell(x, y).Equal(terminal.Cell(x, y)) {
								t.Fatalf("replay differs at %d,%d", x, y)
							}
						}
					}
					data, err = r.Draw(source, nil)
					if err != nil || len(data) != 0 {
						t.Fatalf("committed shadow differs: %q, %v", data, err)
					}
				})
			}
		}
	}
}
