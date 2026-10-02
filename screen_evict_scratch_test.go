package vt

import (
	"testing"

	renderer "github.com/bnema/vev-vt/core"
)

// OnLineEvicted callers may retain the slice: successive evictions must not
// share the Screen's internal scratch row.
func TestOnLineEvictedRowsAreIndependentCopies(t *testing.T) {
	s := NewScreen(4, 2)
	var rows [][]renderer.Cell
	s.OnLineEvicted = func(row []renderer.Cell) { rows = append(rows, row) }
	s.Write([]byte("aaaa\r\nbbbb\r\ncccc\r\ndddd"))
	if len(rows) != 2 {
		t.Fatalf("evicted %d rows, want 2", len(rows))
	}
	if rows[0][0].Rune != 'a' || rows[1][0].Rune != 'b' {
		t.Fatalf("evicted rows = %q, %q", rows[0][0].Rune, rows[1][0].Rune)
	}
	rows[0][0].Rune = 'z'
	if rows[1][0].Rune != 'b' {
		t.Fatal("evicted rows alias each other")
	}
}
