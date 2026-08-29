package html

import (
	"errors"
	"os"
	"testing"

	"github.com/bnema/vev-vt/core"
	"github.com/stretchr/testify/require"
)

func TestRendererSnapshotCommitsTransactionally(t *testing.T) {
	frame := core.NewFrame(3, 1)
	frame.Set(0, 0, core.Cell{Rune: 'A', Style: core.DefaultStyle()})
	frame.Set(1, 0, core.Cell{Rune: '界', Style: core.DefaultStyle()})
	frame.Set(2, 0, core.Cell{Continuation: true, Style: core.DefaultStyle()})

	renderer, err := New(Options{})
	require.NoError(t, err)

	prepared, err := renderer.Prepare(frame, nil, false, Cursor{Visible: true, StyleSet: true})
	require.NoError(t, err)
	require.True(t, prepared.Update().Snapshot)
	require.Len(t, prepared.Update().Rows, 1)
	require.Equal(t, []CellUpdate{
		{Column: 0, Width: 1, Text: "A", Style: 0},
		{Column: 1, Width: 2, Text: "界", Style: 0},
	}, prepared.Update().Rows[0].Cells)
	require.NotContains(t, string(prepared.JSON()), `"kind":0,"rgb"`)

	_, err = renderer.Prepare(frame, nil, false, Cursor{})
	require.ErrorIs(t, err, ErrPendingDraw)
	require.NoError(t, prepared.Commit())
	require.ErrorIs(t, prepared.Commit(), ErrFinalizedDraw)

	unchanged, err := renderer.Prepare(frame, nil, false, Cursor{Visible: true, StyleSet: true})
	require.NoError(t, err)
	require.False(t, unchanged.Update().Snapshot)
	require.Empty(t, unchanged.Update().Rows)
	require.NoError(t, unchanged.Abort())
	require.ErrorIs(t, unchanged.Abort(), ErrFinalizedDraw)

	var nilDraw *PreparedDraw
	require.True(t, errors.Is(nilDraw.Commit(), ErrStaleDraw))
}

func TestPreparedJSONMatchesBrowserFixture(t *testing.T) {
	frame := core.NewFrame(3, 1)
	frame.Set(0, 0, core.Cell{Rune: 'A', Style: core.DefaultStyle()})
	frame.Set(1, 0, core.Cell{Rune: '界', Style: core.DefaultStyle()})
	frame.Set(2, 0, core.Cell{Continuation: true, Style: core.DefaultStyle()})
	renderer, err := New(Options{})
	require.NoError(t, err)
	prepared, err := renderer.Prepare(frame, nil, false, Cursor{Column: 1, Visible: true, Style: 3, StyleSet: true})
	require.NoError(t, err)
	fixture, err := os.ReadFile("../internal/htmlharness/testdata/snapshot.json")
	require.NoError(t, err)
	require.JSONEq(t, string(fixture), string(prepared.JSON()))
}

func TestRendererFindsChangesOutsideDamage(t *testing.T) {
	frame := core.NewFrame(2, 2)
	renderer, err := New(Options{})
	require.NoError(t, err)
	first, err := renderer.Prepare(frame, nil, false, Cursor{})
	require.NoError(t, err)
	require.NoError(t, first.Commit())

	frame.Set(1, 1, core.Cell{Rune: 'X', Style: core.DefaultStyle()})
	prepared, err := renderer.Prepare(frame, []core.Damage{{Kind: core.DamageText, X: 0, Y: 0, Width: 1, Height: 1}}, false, Cursor{})
	require.NoError(t, err)
	require.False(t, prepared.Update().Snapshot)
	require.Equal(t, 1, prepared.Update().Rows[0].Row)
	require.NoError(t, prepared.Commit())
}

func TestRendererSnapshotsScrollAndEmitsCursorOnlyChanges(t *testing.T) {
	frame := core.NewFrame(2, 2)
	renderer, err := New(Options{})
	require.NoError(t, err)
	first, err := renderer.Prepare(frame, nil, false, Cursor{})
	require.NoError(t, err)
	require.NoError(t, first.Commit())

	cursorOnly, err := renderer.Prepare(frame, nil, false, Cursor{Row: 1, Column: 1, Visible: true, Style: 4, StyleSet: true})
	require.NoError(t, err)
	require.Empty(t, cursorOnly.Update().Rows)
	require.NotNil(t, cursorOnly.Update().Rows)
	require.NotNil(t, cursorOnly.Update().Styles)
	require.Contains(t, string(cursorOnly.JSON()), `"rows":[]`)
	require.Contains(t, string(cursorOnly.JSON()), `"styles":[]`)
	require.Equal(t, 1, cursorOnly.Update().Cursor.Row)
	require.NoError(t, cursorOnly.Commit())

	scroll, err := renderer.Prepare(frame, []core.Damage{{Kind: core.DamageScrollUp, X: 0, Y: 0, Width: 2, Height: 2, Count: 1}}, false, Cursor{})
	require.NoError(t, err)
	require.True(t, scroll.Update().Snapshot)
	require.Len(t, scroll.Update().Rows, 2)
	require.NoError(t, scroll.Abort())
}

func TestPreparedDrawReturnsOwnedUpdateAndJSON(t *testing.T) {
	frame := core.NewFrame(1, 1)
	renderer, err := New(Options{})
	require.NoError(t, err)
	prepared, err := renderer.Prepare(frame, nil, false, Cursor{})
	require.NoError(t, err)

	update := prepared.Update()
	encoded := prepared.JSON()
	wantJSON := string(encoded)
	update.Rows[0].Cells[0].Text = "mutated"
	encoded[0] = 'X'
	require.Equal(t, " ", prepared.Update().Rows[0].Cells[0].Text)
	require.JSONEq(t, wantJSON, string(prepared.JSON()))
	require.Equal(t, byte('{'), prepared.JSON()[0])
}

func TestCopiedPreparedDrawSharesFinalization(t *testing.T) {
	frame := core.NewFrame(1, 1)
	renderer, err := New(Options{})
	require.NoError(t, err)
	prepared, err := renderer.Prepare(frame, nil, false, Cursor{})
	require.NoError(t, err)
	copied := *prepared
	require.NoError(t, copied.Commit())
	require.ErrorIs(t, prepared.Abort(), ErrFinalizedDraw)
}

func TestRendererInvalidatesCopiedDrawOnReset(t *testing.T) {
	frame := core.NewFrame(1, 1)
	renderer, err := New(Options{})
	require.NoError(t, err)
	prepared, err := renderer.Prepare(frame, nil, false, Cursor{})
	require.NoError(t, err)
	copied := *prepared

	renderer.Reset()
	require.ErrorIs(t, prepared.Commit(), ErrStaleDraw)
	require.ErrorIs(t, copied.Abort(), ErrStaleDraw)

	next, err := renderer.Prepare(frame, nil, false, Cursor{})
	require.NoError(t, err)
	require.True(t, next.Update().Snapshot)
	require.NoError(t, next.Commit())
}
