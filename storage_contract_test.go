package vt

import (
	"bytes"
	"testing"

	"github.com/bnema/vev-vt/core"
)

func TestVTH3PreservesRawStyleFieldsNeededForMigration(t *testing.T) {
	rgbStyle := core.Style{
		Bold:                 true,
		Foreground:           17,
		Background:           18,
		HasForegroundRGB:     true,
		ForegroundRGB:        core.RGB{R: 1, G: 2, B: 3},
		HasBackgroundRGB:     true,
		BackgroundRGB:        core.RGB{R: 4, G: 5, B: 6},
		HasUnderlineColor:    true,
		UnderlineColor:       19,
		HasUnderlineColorRGB: true,
		UnderlineColorRGB:    core.RGB{R: 7, G: 8, B: 9},
	}
	indexedStyle := core.Style{
		Foreground:        20,
		ForegroundRGB:     core.RGB{R: 10, G: 11, B: 12},
		Background:        21,
		BackgroundRGB:     core.RGB{R: 13, G: 14, B: 15},
		HasUnderlineColor: true,
		UnderlineColor:    22,
		UnderlineColorRGB: core.RGB{R: 16, G: 17, B: 18},
	}
	row := []core.Cell{
		{Rune: ' ', Style: rgbStyle},
		{Rune: '界', Style: indexedStyle},
		{Style: indexedStyle, Continuation: true},
		core.BlankCell(),
	}
	history := NewHistory(HistoryConfig{MaxRows: 1, MaxCells: len(row), ChunkRows: 1})
	if err := history.AppendWithID(row, LineBound{End: 3, Soft: true}, 41); err != nil {
		t.Fatal(err)
	}

	encoded, err := MarshalHistory(history.View())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := UnmarshalHistory(encoded)
	if err != nil {
		t.Fatal(err)
	}

	got := decoded.Row(0)
	if len(got) != len(row) {
		t.Fatalf("decoded row length = %d, want %d", len(got), len(row))
	}
	for i := range row {
		if got[i] != row[i] {
			t.Fatalf("decoded cell %d = %+v, want exact raw value %+v", i, got[i], row[i])
		}
	}
	if gotBound := decoded.Bound(0); gotBound != (LineBound{End: 3, Soft: true}) {
		t.Fatalf("decoded bound = %+v", gotBound)
	}
	if gotID := decoded.RowID(0); gotID != 41 {
		t.Fatalf("decoded row ID = %d, want 41", gotID)
	}
	reencoded, err := MarshalHistory(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reencoded, encoded) {
		t.Fatal("VTH3 decode and re-encode changed canonical bytes")
	}
}
