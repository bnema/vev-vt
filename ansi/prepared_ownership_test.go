package ansi

import (
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
