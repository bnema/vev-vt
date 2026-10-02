package ansi_test

import (
	"bytes"
	"testing"

	vt "github.com/bnema/vev-vt"
	"github.com/bnema/vev-vt/ansi"
)

func TestBidirectionalScrollReplay(t *testing.T) {
	for _, down := range []bool{false, true} {
		for _, count := range []int{1, 3} {
			frame := vt.NewFrame(12, 10)
			for y := range frame.Height {
				frame.FillRow(y, 0, frame.Width, vt.Cell{Rune: rune('a' + y), Style: vt.Style{Foreground: y, Background: -1}})
			}
			r := ansi.New(ansi.Capabilities{})
			terminal := vt.NewScreen(frame.Width, frame.Height)
			initial, err := r.Draw(frame, nil)
			if err != nil {
				t.Fatal(err)
			}
			terminal.Write(initial)
			kind, exposed := vt.DamageScrollUp, 8-count
			if down {
				kind, exposed = vt.DamageScrollDown, 2
				frame.ScrollDown(2, 7, count)
			} else {
				frame.ScrollUp(2, 7, count)
			}
			for y := exposed; y < exposed+count; y++ {
				frame.FillRow(y, 0, frame.Width, vt.Cell{Rune: 'X', Style: vt.DefaultStyle()})
			}
			damage := []vt.Damage{
				{Kind: kind, Y: 2, Width: 12, Height: 6, Count: count},
				{Kind: vt.DamageText, Y: exposed, Width: 12, Height: count},
			}
			prepared, err := r.Prepare(frame, damage, false)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(prepared.Bytes(), []byte("\x1b[3;8r")) {
				t.Fatalf("missing scroll region: %q", prepared.Bytes())
			}
			terminal.Write(prepared.Bytes())
			prepared.Commit()
			for y := range frame.Height {
				for x := range frame.Width {
					if !frame.Cell(x, y).Equal(terminal.Cell(x, y)) {
						t.Fatalf("down=%v count=%d cell %d,%d mismatch", down, count, x, y)
					}
				}
			}
			if err := frame.CheckInvariants(); err != nil {
				t.Fatal(err)
			}
			unchanged, err := r.Draw(frame, nil)
			if err != nil || len(unchanged) != 0 {
				t.Fatalf("committed shadow differs: %q, %v", unchanged, err)
			}
		}
	}
}

func TestDownwardScrollMismatchFallsBack(t *testing.T) {
	committed := vt.NewFrame(10, 8)
	frame := committed.Clone()
	frame.Set(0, 4, vt.Cell{Rune: 'X', Style: vt.DefaultStyle()})
	for _, width := range []int{9, 10} {
		candidate, err := ansi.PlanDelta(frame, []vt.Damage{{Kind: vt.DamageScrollDown, Y: 1, Width: width, Height: 6, Count: 1}}, committed, false)
		if err != nil {
			t.Fatal(err)
		}
		if !candidate.Plan.Snapshot || candidate.Plan.Scroll.Height != 0 {
			t.Fatalf("unsafe scroll accepted: %+v", candidate.Plan)
		}
	}
}

// A stale commit followed by the current draw's commit, or a Reset between
// Prepare and Commit, must never leave a shadow that disagrees with the
// terminal: the next Prepare is a full snapshot.
func TestCommitAfterStaleCommitOrResetKeepsTerminalExact(t *testing.T) {
	for _, tc := range []string{"stale-then-current", "reset-between"} {
		t.Run(tc, func(t *testing.T) {
			frame := vt.NewFrame(10, 4)
			for y := range frame.Height {
				frame.FillRow(y, 0, frame.Width, vt.Cell{Rune: rune('a' + y), Style: vt.DefaultStyle()})
			}
			r := ansi.New(ansi.Capabilities{})
			terminal := vt.NewScreen(frame.Width, frame.Height)
			initial, err := r.Draw(frame, nil)
			if err != nil {
				t.Fatal(err)
			}
			terminal.Write(initial)

			frame.Set(1, 1, vt.Cell{Rune: 'A', Style: vt.DefaultStyle()})
			a, err := r.Prepare(frame, []vt.Damage{{Kind: vt.DamageText, X: 1, Y: 1, Width: 1, Height: 1}}, false)
			if err != nil {
				t.Fatal(err)
			}
			terminal.Write(a.Bytes())
			// The caller reverts the cell before the next draw; only row 2 is damaged.
			frame.Set(1, 1, vt.Cell{Rune: 'b', Style: vt.DefaultStyle()})
			frame.Set(2, 2, vt.Cell{Rune: 'B', Style: vt.DefaultStyle()})
			if tc == "reset-between" {
				r.Reset()
				a.Commit()
			} else {
				b, err := r.Prepare(frame, []vt.Damage{{Kind: vt.DamageText, X: 2, Y: 2, Width: 1, Height: 1}}, false)
				if err != nil {
					t.Fatal(err)
				}
				terminal.Write(b.Bytes())
				a.Commit()
				b.Commit()
			}
			next, err := r.Prepare(frame, nil, false)
			if err != nil {
				t.Fatal(err)
			}
			terminal.Write(next.Bytes())
			next.Commit()
			for y := range frame.Height {
				for x := range frame.Width {
					if !frame.Cell(x, y).Equal(terminal.Cell(x, y)) {
						t.Fatalf("cell %d,%d = %+v, want %+v", x, y, terminal.Cell(x, y), frame.Cell(x, y))
					}
				}
			}
		})
	}
}

func TestStaleCommitForcesSnapshotAndReplaysCorrectly(t *testing.T) {
	for _, deliverB := range []bool{false, true} {
		frame := vt.NewFrame(10, 4)
		for y := range frame.Height {
			frame.FillRow(y, 0, frame.Width, vt.Cell{Rune: rune('a' + y), Style: vt.DefaultStyle()})
		}
		r := ansi.New(ansi.Capabilities{})
		terminal := vt.NewScreen(frame.Width, frame.Height)
		initial, err := r.Draw(frame, nil)
		if err != nil {
			t.Fatal(err)
		}
		terminal.Write(initial)

		frame.Set(1, 1, vt.Cell{Rune: 'A', Style: vt.DefaultStyle()})
		a, err := r.Prepare(frame, []vt.Damage{{Kind: vt.DamageText, X: 1, Y: 1, Width: 1, Height: 1}}, false)
		if err != nil {
			t.Fatal(err)
		}
		terminal.Write(a.Bytes())
		frame.Set(2, 2, vt.Cell{Rune: 'B', Style: vt.Style{Bold: true, Foreground: 2, Background: -1}})
		b, err := r.Prepare(frame, []vt.Damage{{Kind: vt.DamageText, X: 2, Y: 2, Width: 1, Height: 1}}, false)
		if err != nil {
			t.Fatal(err)
		}
		if deliverB {
			terminal.Write(b.Bytes())
		}
		a.Commit() // superseded by b: must drop the committed shadow

		next, err := r.Prepare(frame, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(next.Bytes()) == 0 || !bytes.Contains(next.Bytes(), []byte("a")) || !bytes.Contains(next.Bytes(), []byte("d")) {
			t.Fatalf("deliverB=%v: next prepare is not a full snapshot: %q", deliverB, next.Bytes())
		}
		terminal.Write(next.Bytes())
		next.Commit()
		for y := range frame.Height {
			for x := range frame.Width {
				if !frame.Cell(x, y).Equal(terminal.Cell(x, y)) {
					t.Fatalf("deliverB=%v: cell %d,%d = %+v, want %+v", deliverB, x, y, terminal.Cell(x, y), frame.Cell(x, y))
				}
			}
		}
		unchanged, err := r.Draw(frame, nil)
		if err != nil || len(unchanged) != 0 {
			t.Fatalf("committed shadow differs after snapshot: %q, %v", unchanged, err)
		}
	}
}
