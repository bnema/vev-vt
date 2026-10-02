package vt

import (
	"encoding/base64"
	"math"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/bnema/vev-vt/graphics"
	"github.com/stretchr/testify/require"
)

func screenKittyImageAPC(action string) []byte {
	payload := base64.RawStdEncoding.EncodeToString([]byte{1, 2, 3, 4})
	return []byte("\x1b_Ga=" + action + ",i=1,f=32,s=1,v=1,C=1;" + payload + "\x1b\\")
}

func screenKittyImageAPCWithID(action string, id uint64) []byte {
	payload := base64.RawStdEncoding.EncodeToString([]byte{1, 2, 3, 4})
	return []byte("\x1b_Ga=" + action + ",i=" + strconv.FormatUint(id, 10) + ",f=32,s=1,v=1,C=1;" + payload + "\x1b\\")
}

func screenKittyPutAPC(id uint64) []byte {
	return []byte("\x1b_Ga=p,i=" + strconv.FormatUint(id, 10) + ";\x1b\\")
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

func TestScreenKittyGraphicsWithoutImageIDDoesNotRespond(t *testing.T) {
	screen := NewScreen(16, 3)
	var responses []string
	screen.OnResponse = func(response []byte) { responses = append(responses, string(response)) }
	payload := base64.RawStdEncoding.EncodeToString([]byte{1, 2, 3, 4})

	screen.Write([]byte("\x1b_Ga=T,f=32,s=1,v=1,C=1;" + payload + "\x1b\\"))
	screen.Write([]byte("\x1b_Ga=T,i=0,f=32,s=1,v=1,C=1;" + payload + "\x1b\\"))
	screen.Write([]byte("\x1b_Ga=q,f=24,s=1,v=1;invalid\x1b\\"))

	require.Empty(t, responses)
	require.Equal(t, uint64(2), screen.GraphicsSnapshot().Usage().Placements)
}

func TestScreenKittyIcatDetectionResponses(t *testing.T) {
	screen := NewScreen(80, 24)
	var responses []string
	screen.OnResponse = func(response []byte) { responses = append(responses, string(response)) }

	screen.Write([]byte("\x1b_Ga=q,f=24,s=1,v=1,S=3,i=1;MTIz\x1b\\"))
	screen.Write([]byte("\x1b_Ga=q,f=24,t=t,s=1,v=1,S=47,i=2;L2Rldi9zaG0va2l0dHktdHR5LWdyYXBoaWNzLXByb3RvY29sLTMzMTU3NTkxNjc\x1b\\"))
	screen.Write([]byte("\x1b_Ga=q,f=24,t=s,s=1,v=1,S=18,i=3;aWNhdC1aQlJCWFdNQ0lIQ0ZD\x1b\\"))
	screen.Write([]byte("\x1b[c"))

	require.Equal(t, []string{
		"\x1b_Gi=1;OK\x1b\\",
		"\x1b_Gi=2;ENOTSUP\x1b\\",
		"\x1b_Gi=3;ENOTSUP\x1b\\",
		"\x1b[?62;22c",
	}, responses)
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
	screen.Write(screenKittyImageAPCWithID("T", 2))
	require.Equal(t, uint64(1), screen.GraphicsSnapshot().Usage().Placements)

	screen.Write([]byte("\x1b[?1049l"))
	require.False(t, screen.AltScreenActive())
	require.Equal(t, primary.Usage(), screen.GraphicsSnapshot().Usage())
}

func TestScreenReenteringActiveAlternateClearsGraphicsState(t *testing.T) {
	screen := NewScreen(8, 3)
	screen.Write([]byte("\x1b[?1049h"))
	screen.Write(screenKittyImageAPC("T"))
	before := screen.GraphicsSnapshot()
	require.NotNil(t, before)
	require.Equal(t, uint64(1), before.Usage().Placements)

	screen.Write([]byte("\x1b[?1049h"))
	after := screen.GraphicsSnapshot()
	require.Nil(t, after)
	require.Equal(t, uint64(1), before.Usage().Placements)
}

func TestScreenFullClearAbortsPendingUploadAndClearsPlacements(t *testing.T) {
	screen := NewScreen(8, 3)
	screen.Write(screenKittyImageAPC("T"))
	screen.Write([]byte("\x1b_Ga=T,i=2,f=32,s=1,v=1,m=1,C=1;AQ\x1b\\"))
	require.Equal(t, uint64(1), screen.GraphicsSnapshot().Usage().Placements)

	screen.Write([]byte("\x1b[2J"))
	require.Equal(t, uint64(0), screen.GraphicsSnapshot().Usage().Placements)
	screen.Write([]byte("\x1b_Gm=0;IDBA\x1b\\"))
	graphicsSnapshot := screen.GraphicsSnapshot()
	require.NotNil(t, graphicsSnapshot)
	require.Equal(t, uint64(1), graphicsSnapshot.Usage().Assets)
	require.Equal(t, uint64(0), graphicsSnapshot.Usage().Placements)
}

func TestScreenPendingTransmitDisplayFailsClosedAcrossErrorAndResize(t *testing.T) {
	pending := []byte("\x1b_Ga=T,i=1,f=32,s=1,v=1,m=1,C=1;AQ\x1b\\")
	continuation := []byte("\x1b_Gm=0;IDBA\x1b\\")

	screen := NewScreen(4, 2)
	screen.Write(pending)
	screen.Write([]byte("\x1b_Ga=p,i=1;\x1b\\"))
	screen.Write(continuation)
	require.NotNil(t, screen.GraphicsSnapshot())
	require.Equal(t, uint64(0), screen.GraphicsSnapshot().Usage().Placements)

	screen.Write(pending)
	screen.Resize(5, 2)
	screen.Write(continuation)
	require.NotNil(t, screen.GraphicsSnapshot())
	require.Equal(t, uint64(0), screen.GraphicsSnapshot().Usage().Placements)
}

func TestScreenPendingTransmitDisplayIsScopedToScreenBuffer(t *testing.T) {
	screen := NewScreen(4, 2)
	screen.Write([]byte("\x1b_Ga=T,i=1,f=32,s=1,v=1,m=1,C=1;AQ\x1b\\"))
	screen.Write([]byte("\x1b[?1049h"))
	screen.Write([]byte("\x1b_Gm=0;IDBA\x1b\\"))
	require.NotNil(t, screen.GraphicsSnapshot())
	require.Equal(t, uint64(0), screen.GraphicsSnapshot().Usage().Placements)
	screen.Write([]byte("\x1b[?1049l"))
	screen.Write([]byte("\x1b_Gm=0;IDBA\x1b\\"))
	require.Equal(t, uint64(1), screen.GraphicsSnapshot().Usage().Placements)
}

func TestScreenPartialPixelGeometryDoesNotInventPixelCoordinates(t *testing.T) {
	screen := NewScreen(10, 4)
	screen.SetGeometry(Geometry{Cols: 10, Rows: 4, PixelWidth: 100})
	screen.Write([]byte("\x1b[2;3H"))
	screen.Write(screenKittyImageAPC("T"))
	placement := screen.GraphicsSnapshot().Placements()[0]
	require.Equal(t, graphics.PixelRect{X: 0, Y: 0, Width: 1, Height: 1}, placement.Destination())
}

func TestScreenKittyPlacementSeparatesCursorOriginCropAndPixelOffset(t *testing.T) {
	screen := NewScreen(10, 4)
	screen.SetGeometry(Geometry{Cols: 10, Rows: 4, PixelWidth: 100, PixelHeight: 40})
	screen.Write([]byte("\x1b[2;3H"))
	payload := base64.RawStdEncoding.EncodeToString(make([]byte, 64))
	screen.Write([]byte("\x1b_Ga=T,i=1,f=32,s=4,v=4,x=1,y=1,w=2,h=2,X=3,Y=4,C=1;" + payload + "\x1b\\"))

	placement := screen.GraphicsSnapshot().Placements()[0]
	require.Equal(t, graphics.PixelRect{X: 1, Y: 1, Width: 2, Height: 2}, placement.Source())
	require.Equal(t, graphics.PixelRect{X: 23, Y: 14, Width: 2, Height: 2}, placement.Destination())
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

	screen.Write(screenKittyPutAPC(1))
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
	require.Equal(t, captured.Graphics().Usage(), screen.GraphicsSnapshot().Usage(), "screen graphics snapshot returns the immutable active-scene reference")

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
	require.Nil(t, screen.GraphicsSnapshot())
	require.Nil(t, screen.Snapshot().Graphics())
	require.Equal(t, "text", rowText(screen.Snapshot().Row(0)))
}

func TestScreenKittyDiscardRetainsTerminatorFraming(t *testing.T) {
	screen := NewScreen(16, 2)
	screen.kittyDiscard = true
	screen.Write([]byte("discarded body"))
	screen.Write([]byte("\x1b"))
	screen.Write([]byte("\\after"))
	require.Equal(t, "after           ", rowText(screen.Snapshot().Row(0)))
}

func TestScreenKittyPlacementUsesCursorAndCControlsMovement(t *testing.T) {
	screen := NewScreen(10, 4)
	screen.SetGeometry(Geometry{Cols: 10, Rows: 4, PixelWidth: 100, PixelHeight: 40})
	screen.Write([]byte("\x1b[2;3H"))
	payload := base64.RawStdEncoding.EncodeToString(make([]byte, 16))
	screen.Write([]byte("\x1b_Ga=T,i=1,f=32,s=2,v=2,c=2,r=1,C=1;" + payload + "\x1b\\"))

	snapshot := screen.GraphicsSnapshot()
	require.NotNil(t, snapshot)
	placements := snapshot.Placements()
	require.Len(t, placements, 1)
	require.Equal(t, graphics.PixelRect{X: 20, Y: 10, Width: 2, Height: 2}, placements[0].Destination())
	require.Equal(t, 1, screen.CursorRow())
	require.Equal(t, 2, screen.CursorCol())

	screen.Write([]byte("\x1b_Ga=T,i=2,f=32,s=2,v=2,C=0;" + payload + "\x1b\\"))
	require.Equal(t, 1, screen.CursorRow())
	require.Equal(t, 3, screen.CursorCol())

	screen.Write([]byte("\x1b[3;4H\x1b_Ga=T,i=3,f=32,s=1,v=1,m=1,C=1;AQ\x1b\\"))
	screen.Write([]byte("\x1b_Gm=0;IDBA\x1b\\"))
	placements = screen.GraphicsSnapshot().Placements()
	require.Len(t, placements, 3)
	require.Equal(t, graphics.PixelRect{X: 30, Y: 20, Width: 1, Height: 1}, placements[2].Destination())
	require.Equal(t, 2, screen.CursorRow())
	require.Equal(t, 3, screen.CursorCol())
}

func TestScreenKittyPlacementNormalizesDeferredWrapCursor(t *testing.T) {
	screen := NewScreen(3, 3)
	screen.SetGeometry(Geometry{Cols: 3, Rows: 3, PixelWidth: 30, PixelHeight: 30})
	screen.Write([]byte("abc"))
	payload := base64.RawStdEncoding.EncodeToString(make([]byte, 4))

	screen.Write([]byte("\x1b_Ga=T,i=1,f=32,s=1,v=1,C=1;" + payload + "\x1b\\"))

	placement := screen.GraphicsSnapshot().Placements()[0]
	require.Equal(t, graphics.PixelRect{X: 20, Width: 1, Height: 1}, placement.Destination())
}

func TestScreenKittyNaturalImageHeightMovesCursorBelowPlacement(t *testing.T) {
	screen := NewScreen(10, 5)
	screen.SetGeometry(Geometry{Cols: 10, Rows: 5, PixelWidth: 100, PixelHeight: 50})
	payload := base64.RawStdEncoding.EncodeToString(make([]byte, 15*25*4))

	screen.Write([]byte("\x1b_Ga=T,i=1,f=32,s=15,v=25;" + payload + "\x1b\\"))

	// A 15x25 image occupies 2x3 cells. Kitty leaves the cursor immediately to
	// the right of the placement and on its final occupied row.
	require.Equal(t, 2, screen.CursorRow())
	require.Equal(t, 2, screen.CursorCol())
}

func TestScreenKittyPlacementScrollsWithTallImageCursorAdvance(t *testing.T) {
	screen := NewScreen(10, 5)
	screen.SetGeometry(Geometry{Cols: 10, Rows: 5, PixelWidth: 100, PixelHeight: 50})
	screen.Write([]byte("\x1b[2;1H"))
	payload := base64.RawStdEncoding.EncodeToString(make([]byte, 15*45*4))

	screen.Write([]byte("\x1b_Ga=T,i=1,f=32,s=15,v=45;" + payload + "\x1b\\"))

	placement := screen.GraphicsSnapshot().Placements()[0]
	require.Equal(t, graphics.PixelRect{X: 0, Y: 0, Width: 15, Height: 45}, placement.Destination(), "the placement must scroll with the text buffer")
	require.Equal(t, 4, screen.CursorRow())
	require.Equal(t, 2, screen.CursorCol())
}

func TestScreenKittyPlacementScrollsOnlyWhenContainedInRegion(t *testing.T) {
	screen := NewScreen(10, 6)
	screen.SetGeometry(Geometry{Cols: 10, Rows: 6, PixelWidth: 100, PixelHeight: 60})
	screen.Write([]byte("\x1b[2;5r"))

	insidePayload := base64.RawStdEncoding.EncodeToString(make([]byte, 1*10*4))
	screen.Write([]byte("\x1b[3;1H\x1b_Ga=T,i=1,f=32,s=1,v=10,C=1;" + insidePayload + "\x1b\\"))
	straddlingPayload := base64.RawStdEncoding.EncodeToString(make([]byte, 1*20*4))
	screen.Write([]byte("\x1b[1;1H\x1b_Ga=T,i=2,f=32,s=1,v=20,Y=5,C=1;" + straddlingPayload + "\x1b\\"))

	screen.Write([]byte("\x1b[S"))

	placements := screen.GraphicsSnapshot().Placements()
	require.Len(t, placements, 2)
	require.Equal(t, graphics.PixelRect{X: 0, Y: 10, Width: 1, Height: 10}, placements[0].Destination(), "a placement contained in the margin must scroll")
	require.Equal(t, graphics.PixelRect{X: 0, Y: 5, Width: 1, Height: 20}, placements[1].Destination(), "a placement crossing the margin must remain fixed")
}

func TestScreenKittyPlacementScrollSkipsBottomEdgeOverflow(t *testing.T) {
	screen := NewScreen(1, 2)
	screen.SetGeometry(Geometry{Cols: 1, Rows: 2, PixelWidth: 10, PixelHeight: 20})
	payload := base64.RawStdEncoding.EncodeToString(make([]byte, 1*10*4))
	screen.Write([]byte("\x1b_Ga=T,i=1,f=32,s=1,v=10,C=1;" + payload + "\x1b\\"))
	placement := screen.GraphicsSnapshot().Placements()[0]
	nearLimit := graphics.PixelRect{X: 0, Y: math.MaxInt64 - 11, Width: 1, Height: 10}
	require.NoError(t, screen.graphics.scene.UpdatePlacement(placement.ID(), graphics.PlacementSpec{Destination: nearLimit}))

	require.NotPanics(t, func() { screen.Write([]byte("\x1b[T")) })

	placement = screen.GraphicsSnapshot().Placements()[0]
	require.Equal(t, nearLimit, placement.Destination())
}

// Fragmented APCs reuse the Screen's escape buffer, so retained graphics state
// (assets and pending chunked uploads) must not alias it.
func TestScreenKittyFragmentedAPCsDoNotAliasEscapeBuffer(t *testing.T) {
	screen := NewScreen(16, 3)
	writeByteWise := func(data []byte) {
		for i := range data {
			screen.Write(data[i : i+1])
		}
	}
	enc := func(b ...byte) string { return base64.StdEncoding.EncodeToString(b) }
	writeByteWise([]byte("\x1b_Ga=t,i=1,f=32,s=1,v=1;" + enc(1, 2, 3, 4) + "\x1b\\"))
	// Chunked upload interleaved with other escape sequences reusing the buffer.
	writeByteWise([]byte("\x1b_Ga=t,i=2,f=32,s=1,v=1,m=1;" + enc(9, 9, 9, 9)[:4] + "\x1b\\"))
	writeByteWise([]byte("\x1b[1mtext\x1b[0m"))
	writeByteWise([]byte("\x1b_Gm=0;" + enc(9, 9, 9, 9)[4:] + "\x1b\\"))

	snapshot := screen.GraphicsSnapshot()
	require.NotNil(t, snapshot)
	var got [][]byte
	for _, asset := range snapshot.Assets() {
		got = append(got, asset.Bytes())
	}
	require.ElementsMatch(t, [][]byte{{1, 2, 3, 4}, {9, 9, 9, 9}}, got)
}

func screenAssetBytes(t *testing.T, screen *Screen) [][]byte {
	t.Helper()
	snapshot := screen.GraphicsSnapshot()
	require.NotNil(t, snapshot)
	var got [][]byte
	for _, asset := range snapshot.Assets() {
		got = append(got, asset.Bytes())
	}
	return got
}

// A callback may call Write re-entrantly; the inner Write must not clobber the
// reusable combine buffer the outer Write is still parsing.
func TestScreenWriteReentrantFromCallbackKeepsOuterStreamIntact(t *testing.T) {
	t.Run("split CSI then DA reply writes text", func(t *testing.T) {
		screen := NewScreen(40, 3)
		fired := 0
		screen.OnResponse = func([]byte) {
			fired++
			// Leave a partial escape, then complete it: the second inner
			// Write takes the combine path while the outer Write is mid-parse.
			screen.Write([]byte("\x1b["))
			screen.Write([]byte("1mZZZZZZZZZZ"))
		}
		screen.Write([]byte("\x1b[1"))
		screen.Write([]byte("m\x1b[cBCDEFGH"))
		require.Equal(t, 1, fired)
		require.Equal(t, "ZZZZZZZZZZBCDEFGH", strings.TrimRight(rowText(screen.Snapshot().Row(0)), " "))
	})
	t.Run("split APC response writes upload", func(t *testing.T) {
		screen := NewScreen(40, 3)
		inner := false
		screen.OnResponse = func([]byte) {
			if inner {
				return
			}
			inner = true
			for _, b := range []byte("\x1b_Ga=t,f=32,s=1,v=1;CQkJCQ==\x1b\\") {
				screen.Write([]byte{b})
			}
		}
		screen.Write([]byte("\x1b_Ga=t,i=1,f=32,s=1,v=1;AQID"))
		screen.Write([]byte("BA==\x1b\\XYZ"))
		require.Equal(t, "XYZ", strings.TrimRight(rowText(screen.Snapshot().Row(0)), " "))
		require.ElementsMatch(t, [][]byte{{1, 2, 3, 4}, {9, 9, 9, 9}}, screenAssetBytes(t, screen))
	})
}

// Base64 payloads padded with CR/LF decode into a buffer sized for the raw
// input. The scene must not retain that slack beyond its accounted bytes,
// otherwise untrusted output could hold memory past MaxEncodedBytes.
func TestScreenKittyNewlinePaddedPayloadRetainsOnlyAccountedBytes(t *testing.T) {
	const uploads, padding = 16, 1 << 20
	screen := NewScreen(16, 3)
	pad := strings.Repeat("\n", padding)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := range uploads {
		screen.Write([]byte("\x1b_Ga=t,i=" + strconv.Itoa(i+1) + ",f=32,s=1,v=1;AQID" + pad + "BA==\x1b\\"))
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	require.Equal(t, uint64(4*uploads), screen.graphics.scene.Usage().EncodedBytes)
	for _, asset := range screenAssetBytes(t, screen) {
		require.Equal(t, []byte{1, 2, 3, 4}, asset)
	}
	// Untrimmed decode buffers would retain about 0.75 MiB per upload.
	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	require.Less(t, growth, int64(4<<20), "heap retained by %d padded uploads", uploads)
	runtime.KeepAlive(screen)
}

// The Screen borrows the caller's Write buffer while parsing; nothing it
// retains may alias that buffer once Write returns.
func TestScreenKittyGraphicsDoesNotAliasCallerWriteBuffer(t *testing.T) {
	scribble := func(b []byte) {
		for i := range b {
			b[i] = 0xff
		}
	}
	t.Run("single-shot transmit and display", func(t *testing.T) {
		screen := NewScreen(16, 3)
		buf := []byte("\x1b_Ga=T,i=1,f=32,s=1,v=1;AQIDBA==\x1b\\")
		screen.Write(buf)
		scribble(buf)
		require.Equal(t, [][]byte{{1, 2, 3, 4}}, screenAssetBytes(t, screen))
	})
	t.Run("first and final chunk in one write", func(t *testing.T) {
		screen := NewScreen(16, 3)
		buf := []byte("\x1b_Ga=t,i=1,f=32,s=1,v=1,m=1;AQID\x1b\\\x1b_Gm=0;BA==\x1b\\")
		screen.Write(buf)
		scribble(buf)
		require.Equal(t, [][]byte{{1, 2, 3, 4}}, screenAssetBytes(t, screen))
	})
	t.Run("first chunk write then buffer reuse before final chunk", func(t *testing.T) {
		screen := NewScreen(16, 3)
		buf := []byte("\x1b_Ga=t,i=1,f=32,s=1,v=1,m=1;AQID\x1b\\")
		screen.Write(buf)
		scribble(buf)
		screen.Write([]byte("\x1b_Gm=0;BA==\x1b\\"))
		require.Equal(t, [][]byte{{1, 2, 3, 4}}, screenAssetBytes(t, screen))
	})
}
