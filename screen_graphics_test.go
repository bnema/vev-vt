package vt

import (
	"encoding/base64"
	"testing"

	"github.com/bnema/vev-vt/graphics"
	"github.com/stretchr/testify/require"
)

func screenKittyImageAPC(action string) []byte {
	payload := base64.RawStdEncoding.EncodeToString([]byte{1, 2, 3, 4})
	return []byte("\x1b_Ga=" + action + ",i=1,f=32,s=1,v=1;" + payload + "\x1b\\")
}

func screenKittyImageAPCWithID(action string, id byte) []byte {
	payload := base64.RawStdEncoding.EncodeToString([]byte{1, 2, 3, 4})
	return []byte("\x1b_Ga=" + action + ",i=" + string(id) + ",f=32,s=1,v=1;" + payload + "\x1b\\")
}

func screenKittyPutAPC(id byte) []byte {
	return []byte("\x1b_Ga=p,i=" + string(id) + ";\x1b\\")
}

func TestScreenKittyGraphicsFragmentedAPCAndResponse(t *testing.T) {
	screen := NewScreen(16, 3)
	var responses []string
	screen.OnResponse = func(response []byte) { responses = append(responses, string(response)) }

	input := append([]byte("before"), screenKittyImageAPC("T")...)
	input = append(input, []byte("after")...)
	for _, b := range input {
		screen.Write([]byte{b})
	}

	require.Equal(t, "beforeafter     ", rowText(screen.Snapshot().Row(0)))
	require.Equal(t, []string{"\x1b_Gi=1;OK\x1b\\"}, responses)
	snapshot := screen.GraphicsSnapshot()
	require.NotNil(t, snapshot)
	require.Equal(t, uint64(1), snapshot.Usage().Assets)
	require.Equal(t, uint64(1), snapshot.Usage().Placements)
}

func TestScreenKittyGraphicsPrimaryAndAlternateState(t *testing.T) {
	screen := NewScreen(8, 3)
	screen.Write(screenKittyImageAPC("T"))
	primary := screen.GraphicsSnapshot()
	require.NotNil(t, primary)
	require.Equal(t, uint64(1), primary.Usage().Placements)

	screen.Write([]byte("\x1b[?1049halt"))
	require.True(t, screen.AltScreenActive())
	require.Nil(t, screen.GraphicsSnapshot())
	screen.Write(screenKittyImageAPCWithID("T", '2'))
	require.Equal(t, uint64(1), screen.GraphicsSnapshot().Usage().Placements)

	screen.Write([]byte("\x1b[?1049l"))
	require.False(t, screen.AltScreenActive())
	require.Equal(t, primary.Usage(), screen.GraphicsSnapshot().Usage())
}

func TestScreenKittyGraphicsOrdinaryResetClearScrollAndResize(t *testing.T) {
	screen := NewScreen(3, 2)
	screen.Write(screenKittyImageAPC("T"))

	// Printable text and a scroll do not guess at image anchoring. Existing
	// placements remain available until a graphics-aware policy is supplied.
	screen.Write([]byte("abcdefg"))
	require.Equal(t, uint64(1), screen.GraphicsSnapshot().Usage().Placements)
	screen.Resize(4, 3)
	require.Equal(t, uint64(1), screen.GraphicsSnapshot().Usage().Placements)

	// A full ordinary erase removes visible placements while retaining the
	// protocol image mapping for a later explicit put.
	screen.Write([]byte("\x1b[2J"))
	graphicsSnapshot := screen.GraphicsSnapshot()
	require.NotNil(t, graphicsSnapshot)
	require.Equal(t, uint64(1), graphicsSnapshot.Usage().Assets)
	require.Equal(t, uint64(0), graphicsSnapshot.Usage().Placements)

	screen.Write(screenKittyPutAPC('1'))
	require.Equal(t, uint64(1), screen.GraphicsSnapshot().Usage().Placements)
	screen.Write([]byte("\x1bc"))
	require.Nil(t, screen.GraphicsSnapshot())
}

func TestScreenSnapshotCapturesOwnedActiveGraphicsSnapshot(t *testing.T) {
	screen := NewScreen(4, 2)
	screen.Write(screenKittyImageAPC("T"))
	captured := screen.Snapshot()
	require.NotNil(t, captured.Graphics())
	require.Equal(t, uint64(1), captured.Graphics().Usage().Placements)
	require.Equal(t, captured.Graphics().Usage(), screen.CaptureGraphicsSnapshot().Usage(), "capture returns the immutable active-scene reference")

	screen.Write([]byte("\x1b[2J"))
	require.Equal(t, uint64(1), captured.Graphics().Usage().Placements)
	require.Equal(t, uint64(0), screen.Snapshot().Graphics().Usage().Placements)

	// Graphics snapshots are independent of the text frame and have no Cell or
	// VTH3 representation to invalidate.
	require.Equal(t, graphics.Usage{Assets: 1, Placements: 1, EncodedBytes: 4, DecodedPixels: 1}, captured.Graphics().Usage())
}

func TestScreenOrdinaryTextPathDoesNotAllocateGraphicsState(t *testing.T) {
	screen := NewScreen(4, 2)
	screen.Write([]byte("text"))
	require.Nil(t, screen.GraphicsScene())
	require.Nil(t, screen.Snapshot().GraphicsSnapshot())
	require.Equal(t, "text", rowText(screen.Snapshot().Row(0)))
}
