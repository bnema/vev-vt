package core

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCellPayloadValidation(t *testing.T) {
	for _, tt := range []struct {
		name, grapheme, link string
		valid                bool
	}{
		{"empty", "", "", true},
		{"grapheme and link", "e\u0301", "https://example.com", true},
		{"invalid utf8", "\xff", "", false},
		{"control", "a\n", "", false},
		{"OSC injection", "", "https://x/\x1b\\", false},
		{"C1", "", "x\u009c", false},
		{"grapheme limit", strings.Repeat("x", MaxGraphemeBytes+1), "", false},
		{"link limit", "", strings.Repeat("x", MaxHyperlinkBytes+1), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewCellPayload(tt.grapheme, tt.link)
			if !tt.valid {
				require.ErrorIs(t, err, ErrInvalidCellPayload)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.grapheme, p.Grapheme())
			require.Equal(t, tt.link, p.Hyperlink())
		})
	}
}

func TestPayloadOwnershipAndReclamation(t *testing.T) {
	value, err := NewCellPayload("e\u0301", "https://example.com")
	require.NoError(t, err)
	cell := Cell{Rune: 'e', Style: DefaultStyle(), Payload: value}
	source := NewFrame(3, 2)
	baseline := source.LogicalBytes()
	source.Set(0, 0, cell)
	source.Set(1, 0, cell)
	require.Len(t, source.page.payloadIndex, 1)
	require.Equal(t, baseline+value.LogicalBytes(), source.LogicalBytes())
	clone := source.Clone()
	dst := NewFrame(3, 2)
	other, err := NewCellPayload("", "https://other.example")
	require.NoError(t, err)
	dst.Set(0, 0, Cell{Rune: 'x', Payload: other})
	dst.Set(1, 0, source.Cell(0, 0))
	require.Equal(t, cell, dst.Cell(1, 0))
	source.FillRow(0, 0, 3, BlankCell())
	require.Empty(t, source.page.payloadIndex)
	require.Equal(t, baseline, source.LogicalBytes())
	require.Equal(t, cell, clone.Cell(0, 0))
	require.Equal(t, cell, dst.Cell(1, 0))
	for _, f := range []Frame{source, clone, dst} {
		require.NoError(t, f.CheckInvariants())
	}
	clone.ScrollUp(0, 1, 1)
	require.Empty(t, clone.page.payloadIndex)
	require.NoError(t, clone.CheckInvariants())
}

func TestPayloadChurnIsBounded(t *testing.T) {
	f := NewFrame(1, 1)
	for i := range 1000 {
		p, err := NewCellPayload("", fmt.Sprintf("https://example.com/%d", i))
		require.NoError(t, err)
		f.Set(0, 0, Cell{Rune: 'x', Payload: p})
	}
	require.LessOrEqual(t, len(f.page.payloads), 2)
	require.Len(t, f.page.payloadIndex, 1)
	require.NoError(t, f.CheckInvariants())
	f.page.cells[0].payloadID = 99
	require.Error(t, f.CheckInvariants())
}

func BenchmarkPayloadOverwrite(b *testing.B) {
	values := make([]CellPayload, 128)
	for i := range values {
		var err error
		values[i], err = NewCellPayload("e\u0301", fmt.Sprintf("https://example.com/%d", i))
		if err != nil {
			b.Fatal(err)
		}
	}
	frame := NewFrame(1, 1)
	i := 0
	b.ReportAllocs()
	for b.Loop() {
		frame.Set(0, 0, Cell{Rune: 'e', Style: DefaultStyle(), Payload: values[i%len(values)]})
		i++
	}
}
