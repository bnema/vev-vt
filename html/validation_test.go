package html

import (
	"testing"

	"github.com/bnema/vev-vt/core"
	"github.com/stretchr/testify/require"
)

func TestRendererRejectsMalformedOrOverLimitFramesWithoutPendingState(t *testing.T) {
	renderer, err := New(Options{Limits: Limits{MaxCells: 2}})
	require.NoError(t, err)

	tooLarge := core.NewFrame(3, 1)
	_, err = renderer.Prepare(tooLarge, nil, false, Cursor{})
	require.ErrorIs(t, err, ErrLimitExceeded)

	orphan := core.NewFrame(2, 1)
	orphan.Set(0, 0, core.Cell{Continuation: true, Style: core.DefaultStyle()})
	_, err = renderer.Prepare(orphan, nil, false, Cursor{})
	require.ErrorContains(t, err, "orphan wide continuation")

	combining := core.NewFrame(1, 1)
	combining.Set(0, 0, core.Cell{Rune: '\u0301', Style: core.DefaultStyle()})
	_, err = renderer.Prepare(combining, nil, false, Cursor{})
	require.ErrorContains(t, err, "unsupported zero-width rune")

	wide := core.NewFrame(2, 1)
	wide.Set(0, 0, core.Cell{Rune: '界', Style: core.DefaultStyle()})
	wide.Set(1, 0, core.Cell{Rune: 'X', Continuation: true, Style: core.DefaultStyle()})
	_, err = renderer.Prepare(wide, nil, false, Cursor{})
	require.ErrorContains(t, err, "wide continuation contains a rune")

	alternate := core.DefaultStyle()
	alternate.Bold = true
	wide.Set(1, 0, core.Cell{Continuation: true, Style: alternate})
	_, err = renderer.Prepare(wide, nil, false, Cursor{})
	require.ErrorContains(t, err, "style differs from its head")

	valid := core.NewFrame(2, 1)
	prepared, err := renderer.Prepare(valid, nil, false, Cursor{})
	require.NoError(t, err)
	require.NoError(t, prepared.Abort())
}

func TestNewRejectsNegativeLimits(t *testing.T) {
	_, err := New(Options{Limits: Limits{MaxCells: -1}})
	require.ErrorContains(t, err, "must not be negative")
}

func TestRendererEnforcesEncodedByteAndStyleLimits(t *testing.T) {
	frame := core.NewFrame(2, 1)
	alternate := core.DefaultStyle()
	alternate.Bold = true
	frame.Set(1, 0, core.Cell{Rune: 'X', Style: alternate})

	styleLimited, err := New(Options{Limits: Limits{MaxStyles: 1}})
	require.NoError(t, err)
	_, err = styleLimited.Prepare(frame, nil, false, Cursor{})
	require.ErrorIs(t, err, ErrLimitExceeded)

	byteLimited, err := New(Options{Limits: Limits{MaxGeneratedBytes: 1}})
	require.NoError(t, err)
	_, err = byteLimited.Prepare(core.NewFrame(1, 1), nil, false, Cursor{})
	require.ErrorIs(t, err, ErrLimitExceeded)
	_, err = byteLimited.Prepare(core.NewFrame(1, 1), nil, false, Cursor{})
	require.ErrorIs(t, err, ErrLimitExceeded)
}
