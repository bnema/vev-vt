package graphics

import (
	"errors"
	"math"
	"testing"
)

func testAsset(t *testing.T, data []byte, width, height int64) AssetBlob {
	t.Helper()
	return AssetBlob{Encoded: data, Width: width, Height: height}
}

func TestRectOperationsRejectOverflow(t *testing.T) {
	if _, err := NewPixelRect(math.MaxInt64, 0, 1, 1); !errors.Is(err, ErrInvalidRect) {
		t.Fatalf("overflowing pixel rectangle error = %v", err)
	}
	if _, err := NewCellRect(0, math.MaxInt64, 1, 1); !errors.Is(err, ErrInvalidRect) {
		t.Fatalf("overflowing cell rectangle error = %v", err)
	}
	valid := PixelRect{X: math.MinInt64, Y: math.MinInt64, Width: math.MaxInt64, Height: math.MaxInt64}
	if !valid.Valid() {
		t.Fatal("maximum representable negative-origin rectangle should be valid")
	}
	clipped, ok := valid.Intersect(PixelRect{X: -2, Y: -2, Width: 1, Height: 1})
	if !ok || clipped != (PixelRect{X: -2, Y: -2, Width: 1, Height: 1}) {
		t.Fatalf("intersection = %#v, %v", clipped, ok)
	}
}

func TestAssetBlobOwnershipAndSnapshotLifecycle(t *testing.T) {
	scene := NewScene(Limits{MaxEncodedBytes: 32, MaxDecodedPixels: 64})
	encoded := []byte{1, 2, 3}
	assetID, err := scene.AddAsset(testAsset(t, encoded, 4, 4))
	if err != nil {
		t.Fatal(err)
	}
	encoded[0] = 99

	before := scene.Snapshot()
	asset, ok := before.Asset(assetID)
	if !ok {
		t.Fatal("asset missing from snapshot")
	}
	got := asset.Encoded()
	got[1] = 88
	if got := asset.Encoded(); got[0] != 1 || got[1] != 2 || asset.EncodedSize() != 3 {
		t.Fatalf("asset bytes were not immutable copies: %v", got)
	}

	placementID, err := scene.Place(PlacementSpec{Asset: assetID, Destination: PixelRect{Width: 4, Height: 4}})
	if err != nil {
		t.Fatal(err)
	}
	if err := scene.RemoveAsset(assetID); !errors.Is(err, ErrAssetInUse) {
		t.Fatalf("remove in-use asset error = %v", err)
	}
	if err := scene.RemoveAssetCascade(assetID); err != nil {
		t.Fatal(err)
	}
	if _, ok := scene.Snapshot().Asset(assetID); ok {
		t.Fatal("removed asset remained in current snapshot")
	}
	if _, ok := scene.Snapshot().Placement(placementID); ok {
		t.Fatal("cascade did not remove placement")
	}
	if _, ok := before.Asset(assetID); !ok {
		t.Fatal("old snapshot lost its asset after scene mutation")
	}
}

func TestDecodedPixelAccountingRejectsForgedDeclarations(t *testing.T) {
	scene := NewScene(Limits{MaxEncodedBytes: 64, MaxDecodedPixels: 4})
	_, err := scene.AddAsset(AssetBlob{
		Encoded:       []byte{1},
		Width:         2,
		Height:        2,
		DecodedPixels: 1,
	})
	if !errors.Is(err, ErrInvalidAsset) {
		t.Fatalf("forged decoded-pixel declaration error = %v", err)
	}
	if got := scene.Usage(); got != (Usage{}) {
		t.Fatalf("rejected asset changed usage: %#v", got)
	}
}

func TestAssetAndPlacementQuotasAreTransactional(t *testing.T) {
	scene := NewScene(Limits{
		MaxAssets:        1,
		MaxPlacements:    1,
		MaxEncodedBytes:  4,
		MaxDecodedPixels: 4,
	})
	id, err := scene.AddAsset(testAsset(t, []byte{1, 2, 3, 4}, 2, 2))
	if err != nil {
		t.Fatal(err)
	}
	generation := scene.Snapshot().Generation()
	if _, err := scene.AddAsset(testAsset(t, []byte{5}, 1, 1)); !errors.Is(err, ErrTooManyAssets) {
		t.Fatalf("asset quota error = %v", err)
	}
	if scene.Snapshot().Generation() != generation {
		t.Fatal("failed asset addition changed generation")
	}
	if _, err := scene.Place(PlacementSpec{Asset: id, Destination: PixelRect{Width: 2, Height: 2}}); err != nil {
		t.Fatal(err)
	}
	if _, err := scene.Place(PlacementSpec{Asset: id, Destination: PixelRect{X: 2, Width: 2, Height: 2}}); !errors.Is(err, ErrTooManyPlacements) {
		t.Fatalf("placement quota error = %v", err)
	}
	if err := scene.Apply(
		Operation{Kind: OperationRemovePlacement, PlacementID: PlacementID{value: 1}},
		Operation{Kind: OperationPlace, Placement: PlacementSpec{Asset: id, Destination: PixelRect{Width: 2, Height: 2}}},
	); err != nil {
		t.Fatal(err)
	}
	if got := scene.Usage(); got.Assets != 1 || got.Placements != 1 || got.EncodedBytes != 4 || got.DecodedPixels != 4 {
		t.Fatalf("usage = %#v", got)
	}
}

func TestSnapshotGenerationAndTransactionalApply(t *testing.T) {
	scene := NewScene(Limits{MaxEncodedBytes: 64, MaxDecodedPixels: 64})
	initial := scene.Snapshot()
	id, err := scene.AddAsset(testAsset(t, []byte{1}, 2, 2))
	if err != nil {
		t.Fatal(err)
	}
	if got := scene.Snapshot().Generation(); got <= initial.Generation() {
		t.Fatalf("generation did not advance: initial=%d current=%d", initial.Generation(), got)
	}
	before := scene.Snapshot()
	err = scene.Apply(
		Operation{Kind: OperationPlace, Placement: PlacementSpec{Asset: id, Destination: PixelRect{Width: 2, Height: 2}}},
		Operation{Kind: OperationRemovePlacement, PlacementID: PlacementID{value: 999}},
	)
	if !errors.Is(err, ErrPlacementNotFound) {
		t.Fatalf("transaction error = %v", err)
	}
	if scene.Snapshot().Generation() != before.Generation() || len(scene.Snapshot().Placements()) != 0 {
		t.Fatal("failed transaction partially committed")
	}
}

func TestVisibleFragmentsPreserveScaledSourceMapping(t *testing.T) {
	scene := NewScene(Limits{MaxEncodedBytes: 64, MaxDecodedPixels: 10000})
	assetID, err := scene.AddAsset(testAsset(t, []byte{1}, 100, 50))
	if err != nil {
		t.Fatal(err)
	}
	placementID, err := scene.Place(PlacementSpec{
		Asset:       assetID,
		Destination: PixelRect{X: 10, Y: 20, Width: 200, Height: 100},
		Cells:       CellRect{X: 2, Y: 3, Width: 20, Height: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	fragments := scene.Snapshot().VisibleFragments(PixelRect{X: 110, Y: 20, Width: 100, Height: 100})
	if len(fragments) != 1 {
		t.Fatalf("fragments = %#v", fragments)
	}
	fragment := fragments[0]
	if fragment.PlacementID != placementID || fragment.AssetID != assetID {
		t.Fatalf("fragment IDs = %#v", fragment)
	}
	if fragment.Destination != (PixelRect{X: 110, Y: 20, Width: 100, Height: 100}) {
		t.Fatalf("destination = %#v", fragment.Destination)
	}
	if fragment.Source != (PixelRect{X: 50, Y: 0, Width: 50, Height: 50}) {
		t.Fatalf("source mapping = %#v", fragment.Source)
	}
	if fragment.Cells != (CellRect{X: 12, Y: 3, Width: 10, Height: 10}) {
		t.Fatalf("cell mapping = %#v", fragment.Cells)
	}

	cellFragments := scene.Snapshot().VisibleFragments(CellRect{X: 7, Y: 5, Width: 5, Height: 5})
	if len(cellFragments) != 1 || cellFragments[0].Destination != (PixelRect{X: 60, Y: 40, Width: 50, Height: 50}) {
		t.Fatalf("cell clipping = %#v", cellFragments)
	}
}

func TestVisibleFragmentsRejectInvalidMixedViewport(t *testing.T) {
	scene := NewScene(Limits{MaxEncodedBytes: 64, MaxDecodedPixels: 64})
	assetID, err := scene.AddAsset(testAsset(t, []byte{1}, 2, 2))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scene.Place(PlacementSpec{
		Asset:       assetID,
		Destination: PixelRect{Width: 2, Height: 2},
		Cells:       CellRect{Width: 2, Height: 2},
	}); err != nil {
		t.Fatal(err)
	}
	for _, viewport := range []Viewport{
		{Pixels: PixelRect{Width: 2, Height: 2}, Cells: CellRect{Width: -1, Height: 1}},
		{Pixels: PixelRect{Width: 2, Height: 2}, Cells: CellRect{X: 1}},
		{Pixels: PixelRect{Width: -1, Height: 1}, Cells: CellRect{Width: 2, Height: 2}},
	} {
		if fragments := scene.Snapshot().VisibleFragments(viewport); len(fragments) != 0 {
			t.Fatalf("invalid mixed viewport %#v produced fragments %#v", viewport, fragments)
		}
	}
}

func TestUpdatePlacementCanResetLayerAndClearCells(t *testing.T) {
	scene := NewScene(Limits{MaxEncodedBytes: 64, MaxDecodedPixels: 64})
	assetID, err := scene.AddAsset(testAsset(t, []byte{1}, 4, 4))
	if err != nil {
		t.Fatal(err)
	}
	placementID, err := scene.Place(PlacementSpec{
		Asset:       assetID,
		Destination: PixelRect{Width: 4, Height: 4},
		Cells:       CellRect{X: 1, Y: 2, Width: 3, Height: 2},
		Layer:       7,
		HasLayer:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := scene.UpdatePlacement(placementID, PlacementSpec{HasCells: true, HasLayer: true}); err != nil {
		t.Fatal(err)
	}
	placement, ok := scene.Snapshot().Placement(placementID)
	if !ok {
		t.Fatal("updated placement missing")
	}
	if placement.Layer() != 0 {
		t.Fatalf("layer = %d, want 0", placement.Layer())
	}
	if placement.Cells() != (CellRect{}) {
		t.Fatalf("cells = %#v, want cleared", placement.Cells())
	}
}
