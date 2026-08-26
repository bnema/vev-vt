package graphicsharness

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"image/color"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	vt "github.com/bnema/vev-vt"
	"github.com/bnema/vev-vt/graphics"
)

//go:embed testdata/pngsuite-exif2c08.png.b64
var pngSuiteEXIFBase64 string

//go:embed testdata/demo.apc
var demoAPC []byte

func TestRenderTerminalDemo(t *testing.T) {
	geometry := vt.Geometry{Cols: 4, Rows: 4, PixelWidth: 4, PixelHeight: 4}
	screen := vt.NewScreen(geometry.Cols, geometry.Rows)
	screen.SetGeometry(geometry)
	screen.Write(demoAPC)
	require.Equal(t, uint64(2), screen.GraphicsSnapshot().Usage().Assets)
	require.Equal(t, uint64(2), screen.GraphicsSnapshot().Usage().Placements)
	placements := screen.GraphicsSnapshot().Placements()
	require.Equal(t, graphics.PixelRect{X: 1, Y: 1, Width: 2, Height: 2}, placements[1].Destination())
	require.Equal(t, graphics.PixelRect{Width: 2, Height: 2}, placements[1].Source())
	require.Equal(t, int64(1), placements[1].Layer())

	frame, err := RenderTerminal(demoAPC, geometry)
	require.NoError(t, err)
	require.Equal(t, color.RGBA{22, 62, 143, 255}, frame.RGBAAt(0, 0))
	require.Equal(t, color.RGBA{168, 67, 106, 255}, frame.RGBAAt(1, 1))
}

func TestRenderTerminalAPC(t *testing.T) {
	frame, err := RenderTerminal([]byte("\x1b_Ga=T,i=1,f=32,s=1,v=1,C=1;/wAA/w\x1b\\"), vt.Geometry{Cols: 1, Rows: 1, PixelWidth: 1, PixelHeight: 1})
	require.NoError(t, err)
	require.Equal(t, color.RGBA{255, 0, 0, 255}, frame.RGBAAt(0, 0))
}

func TestRenderDecodesMetadataBearingPNG(t *testing.T) {
	encoded, err := base64.StdEncoding.DecodeString(pngSuiteEXIFBase64)
	require.NoError(t, err)
	require.True(t, bytes.Contains(encoded, []byte("eXIf")), "fixture must retain its eXIf metadata chunk")

	input := []byte("\x1b_Ga=T,i=1,f=100;" + strings.ReplaceAll(pngSuiteEXIFBase64, "\n", "") + "\x1b\\")
	frame, err := RenderTerminal(input, vt.Geometry{Cols: 32, Rows: 32, PixelWidth: 32, PixelHeight: 32})
	require.NoError(t, err)
	require.Equal(t, 32, frame.Bounds().Dx())
	require.Equal(t, 32, frame.Bounds().Dy())
}

func TestRenderRejectsPNGDimensionsDifferentFromSnapshot(t *testing.T) {
	encoded, err := base64.StdEncoding.DecodeString(pngSuiteEXIFBase64)
	require.NoError(t, err)
	scene := graphics.NewScene(graphics.Limits{})
	assetID, err := scene.AddAsset(graphics.AssetBlob{Format: graphics.AssetFormatPNG, Width: 31, Height: 32, Encoded: encoded})
	require.NoError(t, err)
	_, err = scene.Place(graphics.PlacementSpec{Asset: assetID, Destination: graphics.PixelRect{Width: 31, Height: 32}})
	require.NoError(t, err)

	_, err = Render(scene.Snapshot(), graphics.PixelRect{Width: 31, Height: 32})
	require.EqualError(t, err, "decode asset a1: PNG dimensions do not match snapshot")
}

func TestRenderScalesCropsAndClips(t *testing.T) {
	scene := graphics.NewScene(graphics.Limits{})
	assetID, err := scene.AddAsset(graphics.AssetBlob{
		Format: graphics.AssetFormatRGBA,
		Width:  3,
		Height: 2,
		Encoded: []byte{
			255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255,
			0, 255, 255, 255, 255, 0, 255, 255, 255, 255, 0, 255,
		},
	})
	require.NoError(t, err)
	_, err = scene.Place(graphics.PlacementSpec{
		Asset:       assetID,
		Source:      graphics.PixelRect{X: 1, Width: 2, Height: 2},
		Destination: graphics.PixelRect{X: -1, Y: 1, Width: 5, Height: 2},
	})
	require.NoError(t, err)

	frame, err := Render(scene.Snapshot(), graphics.PixelRect{Width: 4, Height: 4})
	require.NoError(t, err)

	for x, want := range []color.RGBA{{0, 255, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}, {0, 0, 255, 255}} {
		require.Equal(t, want, frame.RGBAAt(x, 1))
	}
	for x, want := range []color.RGBA{{255, 0, 255, 255}, {255, 0, 255, 255}, {255, 255, 0, 255}, {255, 255, 0, 255}} {
		require.Equal(t, want, frame.RGBAAt(x, 2))
	}
}

func TestRenderCompositesPlacementsInLayerOrder(t *testing.T) {
	scene := graphics.NewScene(graphics.Limits{})
	background, err := scene.AddAsset(graphics.AssetBlob{Format: graphics.AssetFormatRGBA, Width: 2, Height: 1, Encoded: []byte{0, 0, 255, 255, 0, 0, 255, 255}})
	require.NoError(t, err)
	foreground, err := scene.AddAsset(graphics.AssetBlob{Format: graphics.AssetFormatRGBA, Width: 1, Height: 1, Encoded: []byte{255, 0, 0, 128}})
	require.NoError(t, err)
	_, err = scene.Place(graphics.PlacementSpec{Asset: background, Destination: graphics.PixelRect{Width: 2, Height: 1}, HasLayer: true})
	require.NoError(t, err)
	_, err = scene.Place(graphics.PlacementSpec{Asset: foreground, Destination: graphics.PixelRect{X: 1, Width: 1, Height: 1}, Layer: 1, HasLayer: true})
	require.NoError(t, err)

	frame, err := Render(scene.Snapshot(), graphics.PixelRect{Width: 2, Height: 1})
	require.NoError(t, err)
	require.Equal(t, color.RGBA{0, 0, 255, 255}, frame.RGBAAt(0, 0))
	require.Equal(t, color.RGBA{128, 0, 127, 255}, frame.RGBAAt(1, 0))
}
