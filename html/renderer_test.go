package html

import (
	"errors"
	"math"
	"math/rand"
	"os"
	"testing"
	"unicode/utf8"

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

func TestRendererAcceptsCellSourceWithoutFrameCopy(t *testing.T) {
	frame := core.NewFrame(2, 1)
	frame.Set(0, 0, core.Cell{Rune: 'A', Style: core.DefaultStyle()})
	snapshot := frame.Clone()

	renderer, err := New(Options{})
	require.NoError(t, err)
	prepared, err := renderer.Prepare(cellSourceFunc{
		columns: snapshot.Columns(),
		rows:    snapshot.Rows(),
		cell:    snapshot.Cell,
	}, nil, false, Cursor{})
	require.NoError(t, err)
	require.True(t, prepared.Update().Snapshot)
	require.Equal(t, []CellUpdate{{Column: 0, Width: 2, Text: "A ", Style: 0}}, prepared.Update().Rows[0].Cells)
	require.NoError(t, prepared.Commit())

	_, err = renderer.Prepare(nil, nil, false, Cursor{})
	require.ErrorContains(t, err, "nil cell source")
}

type cellSourceFunc struct {
	columns int
	rows    int
	cell    func(x, y int) core.Cell
}

func (s cellSourceFunc) Columns() int                         { return s.columns }
func (s cellSourceFunc) Rows() int                            { return s.rows }
func (s cellSourceFunc) Cell(x, y int) core.Cell              { return s.cell(x, y) }
func (s cellSourceFunc) At(x, y int) core.Cell                { return s.cell(x, y) }
func (s cellSourceFunc) Row(y int) []core.Cell                { return nil }
func (s cellSourceFunc) WriteRow(y, x int, c []core.Cell) int { return 0 }

func TestRendererNormalizesWrapPendingCursor(t *testing.T) {
	frame := core.NewFrame(3, 2)
	renderer, err := New(Options{})
	require.NoError(t, err)
	prepared, err := renderer.Prepare(frame, nil, false, Cursor{Row: 0, Column: 3, Visible: true})
	require.NoError(t, err)
	require.Equal(t, 2, prepared.Update().Cursor.Column)
	require.NoError(t, prepared.Commit())

	_, err = renderer.Prepare(frame, nil, false, Cursor{Row: 0, Column: 4})
	require.ErrorContains(t, err, "outside")
}

func TestRendererEnforcesExactGeneratedByteLimit(t *testing.T) {
	frame := core.NewFrame(1, 1)
	renderer, err := New(Options{})
	require.NoError(t, err)
	prepared, err := renderer.Prepare(frame, nil, true, Cursor{})
	require.NoError(t, err)
	size := len(prepared.JSON())
	require.NoError(t, prepared.Commit())

	tight, err := New(Options{Limits: Limits{MaxGeneratedBytes: size}})
	require.NoError(t, err)
	accepted, err := tight.Prepare(frame, nil, true, Cursor{})
	require.NoError(t, err)
	require.NoError(t, accepted.Commit())

	strict, err := New(Options{Limits: Limits{MaxGeneratedBytes: size - 1}})
	require.NoError(t, err)
	_, err = strict.Prepare(frame, nil, true, Cursor{})
	require.ErrorIs(t, err, ErrLimitExceeded)
	require.ErrorContains(t, err, "generated update is")
}

func TestRendererMergesASCIICellsIntoStyledTextRuns(t *testing.T) {
	plain := core.DefaultStyle()
	bold := core.DefaultStyle()
	bold.Bold = true
	frame := core.NewFrame(12, 1)
	for x, r := range "ab" {
		frame.Set(x, 0, core.Cell{Rune: r, Style: plain})
	}
	// x=2 stays blank (Rune 0) and joins the plain run as a space.
	frame.Set(3, 0, core.Cell{Rune: '界', Style: plain})
	frame.Set(4, 0, core.Cell{Continuation: true, Style: plain})
	frame.Set(5, 0, core.Cell{Rune: 'é', Style: plain})
	frame.Set(6, 0, core.Cell{Rune: 'c', Style: bold})
	frame.Set(7, 0, core.Cell{Rune: 'd', Style: bold})
	frame.Set(8, 0, core.Cell{Rune: '~', Style: plain})

	renderer, err := New(Options{})
	require.NoError(t, err)
	prepared, err := renderer.Prepare(frame, nil, false, Cursor{})
	require.NoError(t, err)
	require.Equal(t, []CellUpdate{
		{Column: 0, Width: 3, Text: "ab ", Style: 0},
		{Column: 3, Width: 2, Text: "界", Style: 0},
		{Column: 5, Width: 1, Text: "é", Style: 0},
		{Column: 6, Width: 2, Text: "cd", Style: 1},
		{Column: 8, Width: 4, Text: "~   ", Style: 0},
	}, prepared.Update().Rows[0].Cells)
	require.NoError(t, prepared.Commit())
}

func TestRendererIncrementalRunsSliceChangedRowsIndependently(t *testing.T) {
	plain := core.DefaultStyle()
	frame := core.NewFrame(4, 3)
	renderer, err := New(Options{})
	require.NoError(t, err)
	first, err := renderer.Prepare(frame, nil, false, Cursor{})
	require.NoError(t, err)
	require.NoError(t, first.Commit())

	frame.Set(0, 0, core.Cell{Rune: 'a', Style: plain})
	frame.Set(1, 2, core.Cell{Rune: '界', Style: plain})
	frame.Set(2, 2, core.Cell{Continuation: true, Style: plain})
	prepared, err := renderer.Prepare(frame, nil, false, Cursor{})
	require.NoError(t, err)
	rows := prepared.Update().Rows
	require.Len(t, rows, 2)
	require.Equal(t, RowUpdate{Row: 0, Cells: []CellUpdate{{Column: 0, Width: 4, Text: "a   ", Style: 0}}}, rows[0])
	require.Equal(t, RowUpdate{Row: 2, Cells: []CellUpdate{
		{Column: 0, Width: 1, Text: " ", Style: 0},
		{Column: 1, Width: 2, Text: "界", Style: 0},
		{Column: 3, Width: 1, Text: " ", Style: 0},
	}}, rows[1])
	require.NoError(t, prepared.Commit())
}

// TestRendererRunsCoverEveryRowExactly checks the run invariants on random
// frames: entries are contiguous, widths sum to the frame width, and every
// entry wider than one cell is printable ASCII with one byte per column.
func TestRendererRunsCoverEveryRowExactly(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	runes := []rune{0, 'a', 'b', ' ', '~', 'é', '✓', '界'}
	styles := []core.Style{core.DefaultStyle(), func() core.Style { s := core.DefaultStyle(); s.Bold = true; return s }()}
	for iteration := range 200 {
		width, height := 1+rng.Intn(20), 1+rng.Intn(4)
		frame := core.NewFrame(width, height)
		for y := range height {
			for x := 0; x < width; x++ {
				r := runes[rng.Intn(len(runes))]
				style := styles[rng.Intn(len(styles))]
				if core.RuneWidth(r) == 2 {
					if x+1 >= width {
						r = 'w'
					} else {
						frame.Set(x, y, core.Cell{Rune: r, Style: style})
						frame.Set(x+1, y, core.Cell{Continuation: true, Style: style})
						x++
						continue
					}
				}
				frame.Set(x, y, core.Cell{Rune: r, Style: style})
			}
		}
		renderer, err := New(Options{})
		require.NoError(t, err)
		prepared, err := renderer.Prepare(frame, nil, false, Cursor{})
		require.NoError(t, err, "iteration %d", iteration)
		requireRunsMatchFrame(t, prepared.Update(), frame, iteration)

		// An incremental update after one changed cell decodes the same way,
		// through the renderer's reused frame page.
		require.NoError(t, prepared.Commit())
		y := rng.Intn(height)
		frame.Set(0, y, core.Cell{Rune: 'Z', Style: styles[1]})
		if width > 1 && frame.Cell(1, y).Continuation {
			frame.Set(1, y, core.Cell{Rune: ' '})
		}
		prepared, err = renderer.Prepare(frame, nil, false, Cursor{})
		require.NoError(t, err, "iteration %d", iteration)
		update := prepared.Update()
		require.False(t, update.Snapshot)
		require.Len(t, update.Rows, 1)
		requireRunsMatchFrame(t, update, frame, iteration)
	}
}

// requireRunsMatchFrame expands every entry back into cells and compares
// each column's text, style and width with the source frame.
func requireRunsMatchFrame(t *testing.T, update Update, frame core.Frame, iteration int) {
	t.Helper()
	for _, row := range update.Rows {
		column := 0
		for _, cell := range row.Cells {
			require.Equal(t, column, cell.Column, "iteration %d row %d", iteration, row.Row)
			source := frame.Cell(column, row.Row)
			require.Equal(t, styleFromCore(source.Style), update.Styles[cell.Style], "iteration %d (%d,%d)", iteration, column, row.Row)
			if r, _ := utf8.DecodeRuneInString(cell.Text); r >= 0x80 {
				require.Equal(t, string(source.Rune), cell.Text, "iteration %d (%d,%d)", iteration, column, row.Row)
				require.Equal(t, core.RuneWidth(source.Rune), cell.Width, "iteration %d (%d,%d)", iteration, column, row.Row)
				column += cell.Width
				continue
			}
			require.Len(t, cell.Text, cell.Width, "iteration %d text %q", iteration, cell.Text)
			for i := range len(cell.Text) {
				source := frame.Cell(column+i, row.Row)
				want := source.Rune
				if want == 0 {
					want = ' '
				}
				require.Equal(t, string(want), cell.Text[i:i+1], "iteration %d (%d,%d)", iteration, column+i, row.Row)
				require.Equal(t, styleFromCore(source.Style), update.Styles[cell.Style], "iteration %d (%d,%d)", iteration, column+i, row.Row)
			}
			column += cell.Width
		}
		require.Equal(t, frame.Width, column, "iteration %d row %d", iteration, row.Row)
	}
}

func TestRendererAcceptsMaxIntGeneratedBytesLimit(t *testing.T) {
	renderer, err := New(Options{Limits: Limits{MaxGeneratedBytes: math.MaxInt}})
	require.NoError(t, err)
	prepared, err := renderer.Prepare(core.NewFrame(2, 1), nil, true, Cursor{})
	require.NoError(t, err)
	require.NoError(t, prepared.Commit())
}

func TestRendererUsesIncrementalDamageWithNormalCounts(t *testing.T) {
	frame := core.NewFrame(2, 2)
	renderer, err := New(Options{})
	require.NoError(t, err)
	first, err := renderer.Prepare(frame, nil, false, Cursor{})
	require.NoError(t, err)
	require.NoError(t, first.Commit())

	frame.Set(0, 0, core.Cell{Rune: 'X', Style: core.DefaultStyle()})
	prepared, err := renderer.Prepare(frame, []core.Damage{{Kind: core.DamageText, X: 0, Y: 0, Width: 1, Height: 1, Count: 1}}, false, Cursor{})
	require.NoError(t, err)
	require.False(t, prepared.Update().Snapshot)
	require.NoError(t, prepared.Commit())
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

func TestPreparedDrawIsIndependentOfLaterPrepares(t *testing.T) {
	frame := core.NewFrame(4, 3)
	frame.Set(0, 0, core.Cell{Rune: 'A', Style: core.DefaultStyle()})
	renderer, err := New(Options{})
	require.NoError(t, err)

	first, err := renderer.Prepare(frame, nil, false, Cursor{})
	require.NoError(t, err)
	firstUpdate, firstJSON := first.Update(), first.JSON()
	require.NoError(t, first.Commit())

	// The caller mutates its frame in place; neither the earlier draw nor the
	// committed shadow may observe it.
	frame.Set(1, 1, core.Cell{Rune: 'B', Style: core.DefaultStyle()})
	second, err := renderer.Prepare(frame, nil, false, Cursor{})
	require.NoError(t, err)
	require.Equal(t, []int{1}, rowIndexes(second.Update()))
	secondUpdate, secondJSON := second.Update(), second.JSON()
	require.NoError(t, second.Commit())

	frame.Set(2, 2, core.Cell{Rune: 'C', Style: core.DefaultStyle()})
	third, err := renderer.Prepare(frame, nil, false, Cursor{})
	require.NoError(t, err)
	require.Equal(t, []int{2}, rowIndexes(third.Update()))
	require.NoError(t, third.Commit())

	require.Equal(t, firstUpdate, first.Update())
	require.Equal(t, firstJSON, first.JSON())
	require.Equal(t, secondUpdate, second.Update())
	require.Equal(t, secondJSON, second.JSON())
}

func TestAbortThenPrepareKeepsCommittedShadow(t *testing.T) {
	frame := core.NewFrame(3, 2)
	renderer, err := New(Options{})
	require.NoError(t, err)
	first, err := renderer.Prepare(frame, nil, false, Cursor{})
	require.NoError(t, err)
	require.NoError(t, first.Commit())

	frame.Set(0, 1, core.Cell{Rune: 'X', Style: core.DefaultStyle()})
	aborted, err := renderer.Prepare(frame, nil, false, Cursor{})
	require.NoError(t, err)
	abortedJSON := aborted.JSON()
	require.NoError(t, aborted.Abort())

	retry, err := renderer.Prepare(frame, nil, false, Cursor{})
	require.NoError(t, err)
	require.False(t, retry.Update().Snapshot)
	require.Equal(t, []int{1}, rowIndexes(retry.Update()))
	require.Equal(t, abortedJSON, retry.JSON())
	require.NoError(t, retry.Commit())

	steady, err := renderer.Prepare(frame, nil, false, Cursor{})
	require.NoError(t, err)
	require.Empty(t, steady.Update().Rows)
}

func rowIndexes(update Update) []int {
	rows := make([]int, 0, len(update.Rows))
	for _, row := range update.Rows {
		rows = append(rows, row.Row)
	}
	return rows
}
