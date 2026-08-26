package graphics_test

import (
	"errors"
	"testing"

	"github.com/bnema/vev-vt/graphics"
)

func TestExternalConsumerCanBuildAndReadImmutableScene(t *testing.T) {
	scene := graphics.NewScene(graphics.Config{
		MaxAssets:        4,
		MaxPlacements:    4,
		MaxEncodedBytes:  32,
		MaxDecodedPixels: 64,
	})
	data := []byte("encoded")
	assetID, err := scene.RegisterAsset(graphics.AssetInput{Encoded: data, Width: 8, Height: 4})
	if err != nil {
		t.Fatal(err)
	}
	data[0] = 'X'

	placementID, err := scene.AddPlacement(graphics.PlacementSpec{
		Asset:       assetID,
		Destination: graphics.PixelRect{X: -2, Y: 3, Width: 16, Height: 8},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := scene.Snapshot()
	asset, ok := snapshot.GetAsset(assetID)
	if !ok || string(asset.Bytes()) != "encoded" {
		t.Fatalf("asset = %#v, ok=%v", asset, ok)
	}
	placement, ok := snapshot.GetPlacement(placementID)
	if !ok || placement.AssetID() != assetID {
		t.Fatalf("placement = %#v, ok=%v", placement, ok)
	}
	fragments := snapshot.VisiblePixelFragments(graphics.PixelRect{X: 0, Y: 3, Width: 4, Height: 8})
	if len(fragments) != 1 || fragments[0].AssetID != assetID || fragments[0].PlacementID != placementID {
		t.Fatalf("fragments = %#v", fragments)
	}

	if err := scene.RemoveAsset(assetID); !errors.Is(err, graphics.ErrAssetInUse) {
		t.Fatalf("remove in-use asset error = %v", err)
	}
	if err := scene.RemoveAssetCascade(assetID); err != nil {
		t.Fatal(err)
	}
	if _, ok := snapshot.GetAsset(assetID); !ok {
		t.Fatal("immutable snapshot changed after scene mutation")
	}
}
