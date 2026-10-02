package ansi

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreparedSnapshotCommitTransfersOnlyPrivatelyOwnedCells(t *testing.T) {
	r := New(Capabilities{})
	source := NewFrame(2, 1)
	source.Set(0, 0, Cell{Rune: 'a', Style: DefaultStyle()})
	prepared, err := r.Prepare(source, nil, true)
	require.NoError(t, err)
	alias := prepared
	encoded := append([]byte(nil), prepared.Bytes()...)
	source.Set(0, 0, Cell{Rune: 'b', Style: DefaultStyle()})
	prepared.Commit()
	require.Equal(t, 'a', r.committed.Cell(0, 0).Rune)
	_, err = r.Draw(source, []Damage{FullRedraw()})
	require.NoError(t, err)
	alias.Commit()
	require.Equal(t, 'b', r.committed.Cell(0, 0).Rune, "a retained draw copy cannot recommit stale cells")
	require.Equal(t, encoded, alias.Bytes())
}

func TestRendererNeverAliasesCallerFrame(t *testing.T) {
	r := New(Capabilities{})
	frame := NewFrame(6, 3)
	markFrame(&frame)
	_, err := r.Draw(frame, []Damage{FullRedraw()})
	require.NoError(t, err)

	// Mutate the caller's frame in place after commit without drawing, then
	// restore it: the renderer's committed state must not have followed it.
	original := frame.Cell(2, 1)
	frame.Set(2, 1, Cell{Rune: 'Q', Style: Style{Bold: true, Foreground: 1, Background: -1}})
	require.Equal(t, original.Rune, r.committed.Cell(2, 1).Rune)
	frame.Set(2, 1, original)
	prepared, err := r.Prepare(frame, nil, false)
	require.NoError(t, err)
	require.Empty(t, prepared.Bytes(), "committed state followed caller mutation")
	prepared.Commit()

	// Mutate after Prepare and before Commit: the commit keeps the prepared
	// content, so the next draw re-emits the difference.
	frame.Set(0, 0, Cell{Rune: 'P', Style: DefaultStyle()})
	prepared, err = r.Prepare(frame, []Damage{{Kind: DamageText, X: 0, Y: 0, Width: 1, Height: 1}}, false)
	require.NoError(t, err)
	frame.Set(0, 0, Cell{Rune: 'R', Style: DefaultStyle()})
	prepared.Commit()
	require.Equal(t, 'P', r.committed.Cell(0, 0).Rune)
	out, err := r.Draw(frame, nil)
	require.NoError(t, err)
	require.Contains(t, string(out), "R")
	require.Equal(t, 'R', r.committed.Cell(0, 0).Rune)
	require.NoError(t, r.committed.CheckInvariants())
}

func TestDiscardedPreparedDrawLeavesCommittedStateUnchanged(t *testing.T) {
	r := New(Capabilities{})
	frame := NewFrame(5, 3)
	markFrame(&frame)
	_, err := r.Draw(frame, []Damage{FullRedraw()})
	require.NoError(t, err)
	committed := r.committed.Clone()

	for _, damage := range [][]Damage{
		{FullRedraw()},
		{{Kind: DamageText, X: 0, Y: 1, Width: 5, Height: 1}},
		nil,
	} {
		frame.Set(1, 1, Cell{Rune: '#', Style: Style{Italic: true, Foreground: 2, Background: -1}})
		discarded, err := r.Prepare(frame, damage, false)
		require.NoError(t, err)
		require.NotEmpty(t, discarded.Bytes())
		for y := range committed.Height {
			require.Equal(t, committed.Row(y), r.committed.Row(y))
		}
		// Preparing again after a discard produces the same bytes.
		again, err := r.Prepare(frame, damage, false)
		require.NoError(t, err)
		require.Equal(t, discarded.Bytes(), again.Bytes())
		discarded.Commit() // superseded by a later Prepare: must not apply
		for y := range committed.Height {
			require.Equal(t, committed.Row(y), r.committed.Row(y))
		}
		// The stale commit invalidated every outstanding draw, so committing
		// the newer one cannot re-establish a shadow; the next draw is a
		// full snapshot that leaves the shadow exact.
		again.Commit()
		require.False(t, r.hasCommitted)
		snapshot, err := r.Draw(frame, nil)
		require.NoError(t, err)
		require.NotEmpty(t, snapshot)
		require.Equal(t, '#', r.committed.Cell(1, 1).Rune)
		require.NoError(t, r.committed.CheckInvariants())

		// Restore for the next scenario.
		frame.Set(1, 1, committed.Cell(1, 1))
		_, err = r.Draw(frame, nil)
		require.NoError(t, err)
		require.Equal(t, committed.Cell(1, 1).Rune, r.committed.Cell(1, 1).Rune)
	}
}

func TestPreparedBytesSurviveLaterDraws(t *testing.T) {
	r := New(Capabilities{})
	frame := NewFrame(8, 2)
	markFrame(&frame)
	first, err := r.Prepare(frame, []Damage{FullRedraw()}, false)
	require.NoError(t, err)
	want := append([]byte(nil), first.Bytes()...)
	first.Commit()
	for i := range 5 {
		frame.Set(i, 0, Cell{Rune: rune('v' + i%3), Style: DefaultStyle()})
		_, err = r.Draw(frame, []Damage{{Kind: DamageText, X: i, Y: 0, Width: 1, Height: 1}})
		require.NoError(t, err)
	}
	require.Equal(t, want, first.Bytes())
}

func TestRendererRepeatedDrawsMatchFreshRenderer(t *testing.T) {
	// A long mixed sequence on one renderer (buffer reuse, resizes, reset,
	// scroll) must keep emitting exactly what a renderer with an exact
	// reference committed frame would: full redraw after each step equals
	// the fresh-renderer snapshot, and no-damage diffs are empty.
	r := New(Capabilities{})
	frame := NewFrame(7, 4)
	markFrame(&frame)
	steps := []func(){
		func() { frame.Set(3, 2, Cell{Rune: 'a', Style: Style{Bold: true, Foreground: 4, Background: -1}}) },
		func() { frame.ScrollUp(0, 3, 1); frame.FillRow(3, 0, 7, Cell{Rune: 'n', Style: DefaultStyle()}) },
		func() { frame = NewFrame(9, 5); markFrame(&frame) },
		func() { frame.Set(8, 4, Cell{Rune: 'z', Style: DefaultStyle()}) },
		func() {
			frame = NewFrame(7, 4)
			markFrame(&frame)
			frame.Set(0, 0, Cell{Rune: 'k', Style: DefaultStyle()})
		},
	}
	for i, step := range steps {
		step()
		if i == 3 {
			r.Reset()
		}
		_, err := r.Draw(frame, nil)
		require.NoError(t, err)
		for y := range frame.Height {
			require.Equal(t, frame.Row(y), r.committed.Row(y), "step %d row %d", i, y)
		}
		out, err := r.Draw(frame, nil)
		require.NoError(t, err)
		require.Empty(t, out, "step %d", i)
		// Reset keeps equivalence with a brand new renderer.
		r2 := New(Capabilities{})
		want, err := r2.Draw(frame, nil)
		require.NoError(t, err)
		r.Reset()
		got, err := r.Draw(frame, nil)
		require.NoError(t, err)
		require.Equal(t, want, got, "step %d", i)
	}
}

func TestPrepareCommitDoesNotAllocateFrameStorage(t *testing.T) {
	r := New(Capabilities{})
	frame := NewFrame(120, 40)
	markFrame(&frame)
	_, err := r.Draw(frame, []Damage{FullRedraw()})
	require.NoError(t, err)
	_, err = r.Draw(frame, []Damage{FullRedraw()})
	require.NoError(t, err)

	// A whole frame is ~76 KB; steady-state draws must stay far below it.
	toggled := false
	damage := []Damage{{Kind: DamageText, X: 60, Y: 20, Width: 1, Height: 1}}
	allocs := testing.AllocsPerRun(20, func() {
		toggled = !toggled
		ch := 'X'
		if toggled {
			ch = 'Y'
		}
		frame.Set(60, 20, Cell{Rune: ch, Style: DefaultStyle()})
		prepared, err := r.Prepare(frame, damage, false)
		if err != nil {
			t.Fatal(err)
		}
		prepared.Commit()
	})
	require.LessOrEqual(t, allocs, 8.0)
	var m1, m2 runtime.MemStats
	runtime.ReadMemStats(&m1)
	for range 50 {
		frame.Set(60, 20, Cell{Rune: 'W', Style: DefaultStyle()})
		prepared, err := r.Prepare(frame, damage, false)
		require.NoError(t, err)
		prepared.Commit()
		frame.Set(60, 20, Cell{Rune: 'V', Style: DefaultStyle()})
		prepared, err = r.Prepare(frame, damage, false)
		require.NoError(t, err)
		prepared.Commit()
	}
	runtime.ReadMemStats(&m2)
	require.Less(t, (m2.TotalAlloc-m1.TotalAlloc)/100, uint64(8*1024), "per-draw bytes allocated")
}
