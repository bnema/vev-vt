// Package graphics provides a renderer-neutral, bounded graphics scene.
//
// The package stores immutable encoded asset blobs and sparse placements. A
// scene is mutated by its owner and publishes immutable snapshots for
// consumers. It deliberately knows nothing about terminal protocols,
// renderers, image formats, or text state.
package graphics

import "errors"

var (
	ErrInvalidRect        = errors.New("invalid rectangle")
	ErrInvalidAsset       = errors.New("invalid asset")
	ErrInvalidPlacement   = errors.New("invalid placement")
	ErrAssetNotFound      = errors.New("asset not found")
	ErrPlacementNotFound  = errors.New("placement not found")
	ErrAssetInUse         = errors.New("asset is still in use")
	ErrTooManyAssets      = errors.New("asset quota exceeded")
	ErrTooManyPlacements  = errors.New("placement quota exceeded")
	ErrEncodedBudget      = errors.New("encoded byte budget exceeded")
	ErrDecodedPixelBudget = errors.New("decoded pixel budget exceeded")
	ErrGenerationOverflow = errors.New("generation overflow")
	ErrIdentifierOverflow = errors.New("identifier overflow")
	ErrInvalidOperation   = errors.New("invalid graphics operation")
	ErrInvalidSceneLimits = errors.New("invalid scene limits")
)

// AssetID identifies an asset in a scene. Its representation is deliberately
// private: IDs are only meaningful to the scene that issued them and are not
// interchangeable with placement IDs.
type AssetID struct{ value uint64 }

// PlacementID identifies a sparse placement in a scene.
type PlacementID struct{ value uint64 }

// Valid reports whether the ID was issued by a scene.
func (id AssetID) Valid() bool { return id.value != 0 }

// Valid reports whether the ID was issued by a scene.
func (id PlacementID) Valid() bool { return id.value != 0 }

// String returns a stable diagnostic form of the opaque ID.
func (id AssetID) String() string { return formatID('a', id.value) }

// String returns a stable diagnostic form of the opaque ID.
func (id PlacementID) String() string { return formatID('p', id.value) }

func formatID(prefix byte, value uint64) string {
	if value == 0 {
		return "<invalid>"
	}
	var buf [1 + 20]byte
	buf[0] = prefix
	i := len(buf)
	for value != 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[0:1]) + string(buf[i:])
}

// Limits bounds live scene resources. Zero-valued limits use the package
// defaults; this keeps a zero-value configuration bounded as well.
type Limits struct {
	MaxAssets                uint64
	MaxPlacements            uint64
	MaxEncodedBytes          uint64
	MaxDecodedPixels         uint64
	MaxEncodedBytesPerAsset  uint64
	MaxDecodedPixelsPerAsset uint64
}

// Config is an alternate name for Limits for callers that prefer constructor
// configuration terminology.
type Config = Limits

// SceneConfig is a descriptive alias for Config.
type SceneConfig = Limits

// Generation is the monotonically increasing scene generation number.
type Generation = uint64

// SnapshotRef is an explicit name for an immutable snapshot reference.
type SnapshotRef = Snapshot

// AssetInput is an alternate name for an asset registration description.
type AssetInput = AssetBlob

const (
	DefaultMaxAssets                = uint64(1024)
	DefaultMaxPlacements            = uint64(65536)
	DefaultMaxEncodedBytes          = uint64(64 << 20)
	DefaultMaxDecodedPixels         = uint64(256 << 20)
	DefaultMaxEncodedBytesPerAsset  = uint64(16 << 20)
	DefaultMaxDecodedPixelsPerAsset = uint64(128 << 20)
)

// AssetBlob is the caller-owned description of one encoded asset. AddAsset
// copies Encoded before returning, so later caller mutations cannot alter a
// scene or any snapshot. Width and Height describe the decoded pixel extent
// used for clipping and quota accounting.
type AssetBlob struct {
	Encoded       []byte
	Data          []byte
	Width, Height int64
	DecodedPixels uint64
}

// AssetSpec is the conventional name for an asset input description.
type AssetSpec = AssetBlob

// PlacementSpec describes one sparse placement. Source defaults to the full
// asset. Destination is a pixel rectangle; Dest is accepted as a concise alias
// for callers that use that spelling. Cells optionally records the cell-space
// extent associated with the same placement. HasCells and HasLayer preserve
// explicit zero values when updating an existing placement.
type PlacementSpec struct {
	Asset       AssetID
	AssetID     AssetID
	Source      PixelRect
	Destination PixelRect
	Dest        PixelRect
	Cells       CellRect
	CellBounds  CellRect
	HasCells    bool
	Layer       int64
	HasLayer    bool
}

// Viewport can constrain visible fragments in pixel space, cell space, or
// both. A zero-valued rectangle means that coordinate space is not supplied.
type Viewport struct {
	Pixels PixelRect
	Cells  CellRect
}

// VisibleFragment is the visible part of a placement. Source and Destination
// are clipped together, preserving the placement's source-to-destination
// mapping even when the destination is scaled. Cells is populated when the
// placement and viewport carry cell geometry.
type VisibleFragment struct {
	PlacementID PlacementID
	AssetID     AssetID
	Source      PixelRect
	Destination PixelRect
	Cells       CellRect
}

// Fragment is a short alias for VisibleFragment.
type Fragment = VisibleFragment

// Usage reports resources held by the current scene or an immutable snapshot.
type Usage struct {
	Assets        uint64
	Placements    uint64
	EncodedBytes  uint64
	DecodedPixels uint64
}

// OperationKind identifies one transactional scene operation.
type OperationKind uint8

const (
	OperationAddAsset OperationKind = iota + 1
	OperationRemoveAsset
	OperationPlace
	OperationUpdatePlacement
	OperationRemovePlacement
)

// Operation is a sparse scene operation. Add-asset operations allocate an
// opaque ID internally; use AddAsset directly when the ID is needed by the
// caller for a subsequent placement.
type Operation struct {
	Kind        OperationKind
	Blob        AssetBlob
	Asset       AssetID
	AssetID     AssetID
	Placement   PlacementSpec
	PlacementID PlacementID
}

// Operation aliases make operation construction readable without introducing
// protocol-specific terminology.
const (
	AddAssetOperation        = OperationAddAsset
	RemoveAssetOperation     = OperationRemoveAsset
	PlaceOperation           = OperationPlace
	UpdatePlacementOperation = OperationUpdatePlacement
	RemovePlacementOperation = OperationRemovePlacement
)
