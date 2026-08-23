package graphics

import (
	"fmt"
	"math/big"
	"sort"
	"sync"
)

type assetRecord struct {
	id      AssetID
	encoded []byte
	width   int64
	height  int64
	pixels  uint64
}

type placementRecord struct {
	id          PlacementID
	asset       AssetID
	source      PixelRect
	destination PixelRect
	cells       CellRect
	layer       int64
	order       uint64
}

type sceneState struct {
	generation uint64
	nextAsset  uint64
	nextPlace  uint64
	nextOrder  uint64
	assets     map[AssetID]assetRecord
	placements map[PlacementID]placementRecord
	usage      Usage
}

// Scene owns a bounded set of immutable assets and sparse placements. Scene
// methods are safe for concurrent callers; snapshots never share mutable maps
// with the scene. The scene itself contains no text or renderer state.
type Scene struct {
	mu     sync.RWMutex
	limits Limits
	state  *sceneState
}

// NewScene creates an empty bounded graphics scene. Zero-valued limits use
// package defaults. Limits are copied and may be safely reused by the caller.
func NewScene(limits Limits) *Scene {
	return &Scene{limits: normalizeLimits(limits), state: emptyState()}
}

// New is a concise constructor alias.
func New(limits Limits) *Scene { return NewScene(limits) }

// NewSceneWithLimits is a descriptive constructor alias.
func NewSceneWithLimits(limits Limits) *Scene { return NewScene(limits) }

func emptyState() *sceneState {
	return &sceneState{
		nextAsset:  1,
		nextPlace:  1,
		nextOrder:  1,
		assets:     make(map[AssetID]assetRecord),
		placements: make(map[PlacementID]placementRecord),
	}
}

func normalizeLimits(limits Limits) Limits {
	if limits.MaxAssets == 0 {
		limits.MaxAssets = DefaultMaxAssets
	}
	if limits.MaxPlacements == 0 {
		limits.MaxPlacements = DefaultMaxPlacements
	}
	if limits.MaxEncodedBytes == 0 {
		limits.MaxEncodedBytes = DefaultMaxEncodedBytes
	}
	if limits.MaxDecodedPixels == 0 {
		limits.MaxDecodedPixels = DefaultMaxDecodedPixels
	}
	if limits.MaxEncodedBytesPerAsset == 0 {
		limits.MaxEncodedBytesPerAsset = DefaultMaxEncodedBytesPerAsset
	}
	if limits.MaxDecodedPixelsPerAsset == 0 {
		limits.MaxDecodedPixelsPerAsset = DefaultMaxDecodedPixelsPerAsset
	}
	return limits
}

// Limits returns the immutable limits configured for the scene.
func (s *Scene) Limits() Limits {
	if s == nil {
		return Limits{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.limits
}

// AddAsset copies and registers an encoded asset, returning an opaque ID.
func (s *Scene) AddAsset(blob AssetBlob) (AssetID, error) {
	if s == nil {
		return AssetID{}, fmt.Errorf("add asset: %w", ErrInvalidAsset)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := cloneState(s.state)
	record, err := s.prepareAsset(blob, candidate)
	if err != nil {
		return AssetID{}, err
	}
	id, ok := takeAssetID(candidate)
	if !ok {
		return AssetID{}, fmt.Errorf("add asset: %w", ErrIdentifierOverflow)
	}
	record.id = id
	candidate.assets[id] = record
	candidate.usage.Assets++
	candidate.usage.EncodedBytes += uint64(len(record.encoded))
	candidate.usage.DecodedPixels += record.pixels
	if err := advanceGeneration(candidate); err != nil {
		return AssetID{}, err
	}
	s.state = candidate
	return id, nil
}

// RegisterAsset is an alias for AddAsset.
func (s *Scene) RegisterAsset(blob AssetBlob) (AssetID, error) { return s.AddAsset(blob) }

// ReplaceAsset atomically replaces an asset while removing all placements that
// reference it. If validation or resource limits reject the new asset, the
// existing asset and placements remain unchanged.
func (s *Scene) ReplaceAsset(id AssetID, blob AssetBlob) (AssetID, error) {
	if s == nil {
		return AssetID{}, fmt.Errorf("replace asset: %w", ErrAssetNotFound)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := cloneState(s.state)
	old, ok := candidate.assets[id]
	if !ok {
		return AssetID{}, fmt.Errorf("replace asset: %w", ErrAssetNotFound)
	}
	for placementID, placement := range candidate.placements {
		if placement.asset != id {
			continue
		}
		delete(candidate.placements, placementID)
		candidate.usage.Placements--
	}
	delete(candidate.assets, id)
	candidate.usage.Assets--
	candidate.usage.EncodedBytes -= uint64(len(old.encoded))
	candidate.usage.DecodedPixels -= old.pixels
	replacement, err := s.prepareAsset(blob, candidate)
	if err != nil {
		return AssetID{}, err
	}
	replacementID, ok := takeAssetID(candidate)
	if !ok {
		return AssetID{}, fmt.Errorf("replace asset: %w", ErrIdentifierOverflow)
	}
	replacement.id = replacementID
	candidate.assets[replacementID] = replacement
	candidate.usage.Assets++
	candidate.usage.EncodedBytes += uint64(len(replacement.encoded))
	candidate.usage.DecodedPixels += replacement.pixels
	if err := advanceGeneration(candidate); err != nil {
		return AssetID{}, fmt.Errorf("replace asset: %w", err)
	}
	s.state = candidate
	return replacementID, nil
}

// AddEncodedAsset registers an asset from its encoded bytes and decoded pixel
// dimensions. The input bytes are copied before this method returns.
func (s *Scene) AddEncodedAsset(encoded []byte, width, height int64) (AssetID, error) {
	return s.AddAsset(AssetBlob{Encoded: encoded, Width: width, Height: height})
}

// RemoveAsset removes an unreferenced asset. Use RemoveAssetCascade when the
// asset's placements should be removed as one transaction.
func (s *Scene) RemoveAsset(id AssetID) error {
	if s == nil {
		return fmt.Errorf("remove asset: %w", ErrAssetNotFound)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := cloneState(s.state)
	if err := removeAsset(candidate, id, false); err != nil {
		return err
	}
	if err := advanceGeneration(candidate); err != nil {
		return fmt.Errorf("remove asset: %w", err)
	}
	s.state = candidate
	return nil
}

// RemoveAssetCascade removes an asset and all placements that reference it in
// one generation.
func (s *Scene) RemoveAssetCascade(id AssetID) error {
	if s == nil {
		return fmt.Errorf("remove asset: %w", ErrAssetNotFound)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := cloneState(s.state)
	if err := removeAsset(candidate, id, true); err != nil {
		return err
	}
	if err := advanceGeneration(candidate); err != nil {
		return fmt.Errorf("remove asset: %w", err)
	}
	s.state = candidate
	return nil
}

// Place adds one sparse placement and returns its opaque ID.
func (s *Scene) Place(spec PlacementSpec) (PlacementID, error) {
	if s == nil {
		return PlacementID{}, fmt.Errorf("place: %w", ErrInvalidPlacement)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := cloneState(s.state)
	record, err := s.preparePlacement(candidate, spec, nil)
	if err != nil {
		return PlacementID{}, err
	}
	if uint64(len(candidate.placements)) >= s.limits.MaxPlacements {
		return PlacementID{}, fmt.Errorf("place: %w", ErrTooManyPlacements)
	}
	id, ok := takePlacementID(candidate)
	if !ok {
		return PlacementID{}, fmt.Errorf("place: %w", ErrIdentifierOverflow)
	}
	record.id = id
	record.order = takeOrder(candidate)
	candidate.placements[id] = record
	candidate.usage.Placements++
	if err := advanceGeneration(candidate); err != nil {
		return PlacementID{}, fmt.Errorf("place: %w", err)
	}
	s.state = candidate
	return id, nil
}

// AddPlacement is an alias for Place.
func (s *Scene) AddPlacement(spec PlacementSpec) (PlacementID, error) { return s.Place(spec) }

// PlaceAsset creates a full-source placement of an asset.
func (s *Scene) PlaceAsset(asset AssetID, destination PixelRect) (PlacementID, error) {
	return s.Place(PlacementSpec{Asset: asset, Destination: destination})
}

// UpdatePlacement replaces the geometry and optional metadata of a placement.
// If spec.Asset and spec.AssetID are both invalid, the existing asset is kept.
func (s *Scene) UpdatePlacement(id PlacementID, spec PlacementSpec) error {
	if s == nil {
		return fmt.Errorf("update placement: %w", ErrPlacementNotFound)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := cloneState(s.state)
	existing, ok := candidate.placements[id]
	if !ok {
		return fmt.Errorf("update placement: %w", ErrPlacementNotFound)
	}
	record, err := s.preparePlacement(candidate, spec, &existing)
	if err != nil {
		return err
	}
	record.id = id
	record.order = existing.order
	candidate.placements[id] = record
	if err := advanceGeneration(candidate); err != nil {
		return fmt.Errorf("update placement: %w", err)
	}
	s.state = candidate
	return nil
}

// MovePlacement changes only the destination rectangle of a placement.
func (s *Scene) MovePlacement(id PlacementID, destination PixelRect) error {
	return s.UpdatePlacement(id, PlacementSpec{Destination: destination})
}

// RemovePlacement removes one sparse placement.
func (s *Scene) RemovePlacement(id PlacementID) error {
	if s == nil {
		return fmt.Errorf("remove placement: %w", ErrPlacementNotFound)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := cloneState(s.state)
	if _, ok := candidate.placements[id]; !ok {
		return fmt.Errorf("remove placement: %w", ErrPlacementNotFound)
	}
	delete(candidate.placements, id)
	candidate.usage.Placements--
	if err := advanceGeneration(candidate); err != nil {
		return fmt.Errorf("remove placement: %w", err)
	}
	s.state = candidate
	return nil
}

// Clear removes every asset and placement while preserving the monotonic ID
// counters. It publishes a new generation only when the scene is non-empty.
func (s *Scene) Clear() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.state.assets) == 0 && len(s.state.placements) == 0 {
		return nil
	}
	candidate := cloneState(s.state)
	candidate.assets = make(map[AssetID]assetRecord)
	candidate.placements = make(map[PlacementID]placementRecord)
	candidate.usage = Usage{}
	if err := advanceGeneration(candidate); err != nil {
		return fmt.Errorf("clear scene: %w", err)
	}
	s.state = candidate
	return nil
}

// Apply commits sparse operations transactionally. If any operation fails,
// neither the state nor the generation changes.
func (s *Scene) Apply(operations ...Operation) error {
	if s == nil {
		return fmt.Errorf("apply operations: %w", ErrInvalidOperation)
	}
	if len(operations) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := cloneState(s.state)
	for i, operation := range operations {
		if err := s.applyOperation(candidate, operation); err != nil {
			return fmt.Errorf("apply operation %d: %w", i, err)
		}
	}
	if err := advanceGeneration(candidate); err != nil {
		return fmt.Errorf("apply operations: %w", err)
	}
	s.state = candidate
	return nil
}

// ApplyOperations is the slice-taking form of Apply.
func (s *Scene) ApplyOperations(operations []Operation) error { return s.Apply(operations...) }

// Snapshot returns an immutable reference to the current scene state. Future
// scene mutations use copy-on-write maps and cannot alter this snapshot.
func (s *Scene) Snapshot() *Snapshot {
	if s == nil {
		return &Snapshot{state: emptyState()}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return &Snapshot{state: s.state}
}

// Generation returns the current scene generation.
func (s *Scene) Generation() Generation {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.generation
}

// Usage returns the current live resource usage.
func (s *Scene) Usage() Usage {
	if s == nil {
		return Usage{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.usage
}

func (s *Scene) prepareAsset(blob AssetBlob, state *sceneState) (assetRecord, error) {
	data := blob.Encoded
	if data == nil {
		data = blob.Data
	}
	if len(data) == 0 && blob.Encoded != nil && blob.Data != nil && len(blob.Data) != 0 {
		data = blob.Data
	}
	if blob.Width <= 0 || blob.Height <= 0 {
		return assetRecord{}, fmt.Errorf("add asset: %w", ErrInvalidAsset)
	}
	pixels, ok := decodedPixels(blob.Width, blob.Height, blob.DecodedPixels)
	if !ok {
		return assetRecord{}, fmt.Errorf("add asset: %w", ErrInvalidAsset)
	}
	encoded := uint64(len(data))
	if encoded > s.limits.MaxEncodedBytesPerAsset {
		return assetRecord{}, fmt.Errorf("add asset: %w", ErrEncodedBudget)
	}
	if pixels > s.limits.MaxDecodedPixelsPerAsset {
		return assetRecord{}, fmt.Errorf("add asset: %w", ErrDecodedPixelBudget)
	}
	if uint64(len(state.assets)) >= s.limits.MaxAssets {
		return assetRecord{}, fmt.Errorf("add asset: %w", ErrTooManyAssets)
	}
	if !within(state.usage.EncodedBytes, encoded, s.limits.MaxEncodedBytes) {
		return assetRecord{}, fmt.Errorf("add asset: %w", ErrEncodedBudget)
	}
	if !within(state.usage.DecodedPixels, pixels, s.limits.MaxDecodedPixels) {
		return assetRecord{}, fmt.Errorf("add asset: %w", ErrDecodedPixelBudget)
	}
	return assetRecord{encoded: append([]byte(nil), data...), width: blob.Width, height: blob.Height, pixels: pixels}, nil
}

func (s *Scene) preparePlacement(state *sceneState, spec PlacementSpec, existing *placementRecord) (placementRecord, error) {
	asset := spec.Asset
	if !asset.Valid() {
		asset = spec.AssetID
	}
	if !asset.Valid() && existing != nil {
		asset = existing.asset
	}
	recordAsset, ok := state.assets[asset]
	if !ok {
		return placementRecord{}, fmt.Errorf("place: %w", ErrAssetNotFound)
	}
	destination := spec.Destination
	if destination == (PixelRect{}) {
		destination = spec.Dest
	}
	if existing != nil && destination == (PixelRect{}) {
		destination = existing.destination
	}
	if !destination.Valid() || destination.Empty() {
		return placementRecord{}, fmt.Errorf("place: %w", ErrInvalidPlacement)
	}
	source := spec.Source
	if source == (PixelRect{}) {
		if existing != nil && spec.Source == (PixelRect{}) {
			source = existing.source
		} else {
			source = PixelRect{Width: recordAsset.width, Height: recordAsset.height}
		}
	}
	if !source.Valid() || source.Empty() {
		return placementRecord{}, fmt.Errorf("place: %w", ErrInvalidPlacement)
	}
	assetBounds := PixelRect{Width: recordAsset.width, Height: recordAsset.height}
	if _, ok := assetBounds.Intersect(source); !ok || source != assetBounds && !containsRect(assetBounds, source) {
		return placementRecord{}, fmt.Errorf("place: %w", ErrInvalidPlacement)
	}
	cells := spec.Cells
	if cells == (CellRect{}) {
		cells = spec.CellBounds
	}
	if existing != nil && !spec.HasCells && cells == (CellRect{}) {
		cells = existing.cells
	}
	if cells != (CellRect{}) && (!cells.Valid() || cells.Empty()) {
		return placementRecord{}, fmt.Errorf("place: %w", ErrInvalidPlacement)
	}
	layer := spec.Layer
	if existing != nil && !spec.HasLayer {
		layer = existing.layer
	}
	return placementRecord{asset: asset, source: source, destination: destination, cells: cells, layer: layer}, nil
}

func containsRect(outer, inner PixelRect) bool {
	right, okRight := outer.Right()
	innerRight, okInnerRight := inner.Right()
	bottom, okBottom := outer.Bottom()
	innerBottom, okInnerBottom := inner.Bottom()
	return okRight && okInnerRight && okBottom && okInnerBottom && inner.X >= outer.X && innerRight <= right && inner.Y >= outer.Y && innerBottom <= bottom
}

func removeAsset(state *sceneState, id AssetID, cascade bool) error {
	record, ok := state.assets[id]
	if !ok {
		return fmt.Errorf("remove asset: %w", ErrAssetNotFound)
	}
	for placementID, placement := range state.placements {
		if placement.asset != id {
			continue
		}
		if !cascade {
			return fmt.Errorf("remove asset %s: %w", id, ErrAssetInUse)
		}
		delete(state.placements, placementID)
		state.usage.Placements--
	}
	delete(state.assets, id)
	state.usage.Assets--
	state.usage.EncodedBytes -= uint64(len(record.encoded))
	state.usage.DecodedPixels -= record.pixels
	return nil
}

func (s *Scene) applyOperation(state *sceneState, operation Operation) error {
	switch operation.Kind {
	case OperationAddAsset:
		record, err := s.prepareAsset(operation.Blob, state)
		if err != nil {
			return err
		}
		id, ok := takeAssetID(state)
		if !ok {
			return fmt.Errorf("add asset: %w", ErrIdentifierOverflow)
		}
		record.id = id
		state.assets[id] = record
		state.usage.Assets++
		state.usage.EncodedBytes += uint64(len(record.encoded))
		state.usage.DecodedPixels += record.pixels
	case OperationRemoveAsset:
		id := operation.Asset
		if !id.Valid() {
			id = operation.AssetID
		}
		if err := removeAsset(state, id, false); err != nil {
			return err
		}
	case OperationPlace:
		record, err := s.preparePlacement(state, operation.Placement, nil)
		if err != nil {
			return err
		}
		if uint64(len(state.placements)) >= s.limits.MaxPlacements {
			return fmt.Errorf("place: %w", ErrTooManyPlacements)
		}
		id, ok := takePlacementID(state)
		if !ok {
			return fmt.Errorf("place: %w", ErrIdentifierOverflow)
		}
		record.id = id
		record.order = takeOrder(state)
		state.placements[id] = record
		state.usage.Placements++
	case OperationUpdatePlacement:
		existing, ok := state.placements[operation.PlacementID]
		if !ok {
			return fmt.Errorf("update placement: %w", ErrPlacementNotFound)
		}
		record, err := s.preparePlacement(state, operation.Placement, &existing)
		if err != nil {
			return err
		}
		record.id = existing.id
		record.order = existing.order
		state.placements[existing.id] = record
	case OperationRemovePlacement:
		if _, ok := state.placements[operation.PlacementID]; !ok {
			return fmt.Errorf("remove placement: %w", ErrPlacementNotFound)
		}
		delete(state.placements, operation.PlacementID)
		state.usage.Placements--
	default:
		return fmt.Errorf("operation kind %d: %w", operation.Kind, ErrInvalidOperation)
	}
	return nil
}

func cloneState(state *sceneState) *sceneState {
	if state == nil {
		return emptyState()
	}
	clone := *state
	clone.assets = make(map[AssetID]assetRecord, len(state.assets))
	for id, record := range state.assets {
		clone.assets[id] = record
	}
	clone.placements = make(map[PlacementID]placementRecord, len(state.placements))
	for id, record := range state.placements {
		clone.placements[id] = record
	}
	return &clone
}

func advanceGeneration(state *sceneState) error {
	if state.generation == ^uint64(0) {
		return ErrGenerationOverflow
	}
	state.generation++
	return nil
}

func takeAssetID(state *sceneState) (AssetID, bool) {
	if state.nextAsset == 0 {
		return AssetID{}, false
	}
	id := AssetID{value: state.nextAsset}
	if state.nextAsset == ^uint64(0) {
		state.nextAsset = 0
	} else {
		state.nextAsset++
	}
	return id, true
}

func takePlacementID(state *sceneState) (PlacementID, bool) {
	if state.nextPlace == 0 {
		return PlacementID{}, false
	}
	id := PlacementID{value: state.nextPlace}
	if state.nextPlace == ^uint64(0) {
		state.nextPlace = 0
	} else {
		state.nextPlace++
	}
	return id, true
}

func takeOrder(state *sceneState) uint64 {
	order := state.nextOrder
	if state.nextOrder != ^uint64(0) {
		state.nextOrder++
	} else {
		state.nextOrder = 0
	}
	return order
}

func within(current, additional, limit uint64) bool {
	if current > limit {
		return false
	}
	return additional <= limit-current
}

func decodedPixels(width, height int64, declared uint64) (uint64, bool) {
	if width <= 0 || height <= 0 {
		return 0, false
	}
	w, h := uint64(width), uint64(height)
	if h != 0 && w > ^uint64(0)/h {
		return 0, false
	}
	computed := w * h
	// DecodedPixels is retained as an optional compatibility field, but it is
	// only a valid declaration when it agrees with the dimensions. Resource
	// accounting must never trust a caller-supplied number that can undercount
	// the pixels represented by the asset.
	if declared != 0 && declared != computed {
		return 0, false
	}
	return computed, true
}

// AssetView is an immutable view of a registered asset. Encoded returns a new
// byte slice on every call; no caller can mutate scene or snapshot storage.
type AssetView struct {
	id      AssetID
	encoded []byte
	width   int64
	height  int64
	pixels  uint64
}

// Asset is an alias for the immutable snapshot view.
type Asset = AssetView

func (a AssetView) ID() AssetID           { return a.id }
func (a AssetView) Width() int64          { return a.width }
func (a AssetView) Height() int64         { return a.height }
func (a AssetView) DecodedPixels() uint64 { return a.pixels }
func (a AssetView) EncodedSize() uint64   { return uint64(len(a.encoded)) }
func (a AssetView) Encoded() []byte       { return append([]byte(nil), a.encoded...) }
func (a AssetView) Bytes() []byte         { return a.Encoded() }
func (a AssetView) Blob() AssetBlob {
	return AssetBlob{Encoded: a.Encoded(), Width: a.width, Height: a.height, DecodedPixels: a.pixels}
}

// PlacementView is an immutable view of a sparse placement.
type PlacementView struct {
	id          PlacementID
	asset       AssetID
	source      PixelRect
	destination PixelRect
	cells       CellRect
	layer       int64
}

// Placement is an alias for the immutable snapshot view.
type Placement = PlacementView

func (p PlacementView) ID() PlacementID        { return p.id }
func (p PlacementView) AssetID() AssetID       { return p.asset }
func (p PlacementView) Source() PixelRect      { return p.source }
func (p PlacementView) Destination() PixelRect { return p.destination }
func (p PlacementView) Cells() CellRect        { return p.cells }
func (p PlacementView) Layer() int64           { return p.layer }
func (p PlacementView) Spec() PlacementSpec {
	return PlacementSpec{
		Asset:       p.asset,
		Source:      p.source,
		Destination: p.destination,
		Cells:       p.cells,
		HasCells:    p.cells != (CellRect{}),
		Layer:       p.layer,
		HasLayer:    true,
	}
}

// Snapshot is an immutable reference to a scene state.
type Snapshot struct{ state *sceneState }

// Generation is a monotonically increasing scene generation.
func (s *Snapshot) Generation() uint64 {
	if s == nil || s.state == nil {
		return 0
	}
	return s.state.generation
}

// Usage reports the resources retained by this snapshot's state.
func (s *Snapshot) Usage() Usage {
	if s == nil || s.state == nil {
		return Usage{}
	}
	return s.state.usage
}

// Asset returns an immutable asset view by ID.
func (s *Snapshot) Asset(id AssetID) (AssetView, bool) {
	if s == nil || s.state == nil {
		return AssetView{}, false
	}
	record, ok := s.state.assets[id]
	if !ok {
		return AssetView{}, false
	}
	return AssetView(record), true
}

// GetAsset is an alias for Asset.
func (s *Snapshot) GetAsset(id AssetID) (AssetView, bool) { return s.Asset(id) }

// Assets returns immutable asset views in ID order.
func (s *Snapshot) Assets() []AssetView {
	if s == nil || s.state == nil || len(s.state.assets) == 0 {
		return nil
	}
	ids := make([]AssetID, 0, len(s.state.assets))
	for id := range s.state.assets {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].value < ids[j].value })
	assets := make([]AssetView, 0, len(ids))
	for _, id := range ids {
		asset, _ := s.Asset(id)
		assets = append(assets, asset)
	}
	return assets
}

// Placement returns an immutable placement view by ID.
func (s *Snapshot) Placement(id PlacementID) (PlacementView, bool) {
	if s == nil || s.state == nil {
		return PlacementView{}, false
	}
	record, ok := s.state.placements[id]
	if !ok {
		return PlacementView{}, false
	}
	return PlacementView{id: record.id, asset: record.asset, source: record.source, destination: record.destination, cells: record.cells, layer: record.layer}, true
}

// GetPlacement is an alias for Placement.
func (s *Snapshot) GetPlacement(id PlacementID) (PlacementView, bool) { return s.Placement(id) }

// Placements returns immutable placement views in layer and insertion order.
func (s *Snapshot) Placements() []PlacementView {
	if s == nil || s.state == nil || len(s.state.placements) == 0 {
		return nil
	}
	placements := make([]placementRecord, 0, len(s.state.placements))
	for _, placement := range s.state.placements {
		placements = append(placements, placement)
	}
	sort.SliceStable(placements, func(i, j int) bool {
		if placements[i].layer != placements[j].layer {
			return placements[i].layer < placements[j].layer
		}
		return placements[i].order < placements[j].order
	})
	views := make([]PlacementView, 0, len(placements))
	for _, placement := range placements {
		views = append(views, PlacementView{id: placement.id, asset: placement.asset, source: placement.source, destination: placement.destination, cells: placement.cells, layer: placement.layer})
	}
	return views
}

// VisibleFragments returns clipped placement fragments. viewport may be a
// PixelRect, CellRect, or Viewport. Unsupported or invalid viewport values
// produce no fragments rather than wrapping coordinates.
func (s *Snapshot) VisibleFragments(viewport any) []VisibleFragment {
	if s == nil || s.state == nil {
		return nil
	}
	pixels, cells, hasPixels, hasCells := viewportParts(viewport)
	if !hasPixels && !hasCells {
		return nil
	}
	placements := s.Placements()
	fragments := make([]VisibleFragment, 0, len(placements))
	for _, placement := range placements {
		visible, ok := visiblePlacement(placement, pixels, cells, hasPixels, hasCells)
		if ok {
			fragments = append(fragments, visible)
		}
	}
	return fragments
}

// VisiblePixelFragments is the typed pixel-space form of VisibleFragments.
func (s *Snapshot) VisiblePixelFragments(viewport PixelRect) []VisibleFragment {
	return s.VisibleFragments(viewport)
}

// VisibleCellFragments is the typed cell-space form of VisibleFragments.
func (s *Snapshot) VisibleCellFragments(viewport CellRect) []VisibleFragment {
	return s.VisibleFragments(viewport)
}

func viewportParts(viewport any) (PixelRect, CellRect, bool, bool) {
	switch value := viewport.(type) {
	case PixelRect:
		return value, CellRect{}, value.Valid() && !value.Empty(), false
	case CellRect:
		return PixelRect{}, value, false, value.Valid() && !value.Empty()
	case Viewport:
		hasPixels := value.Pixels != (PixelRect{})
		hasCells := value.Cells != (CellRect{})
		if hasPixels && (!value.Pixels.Valid() || value.Pixels.Empty()) {
			return PixelRect{}, CellRect{}, false, false
		}
		if hasCells && (!value.Cells.Valid() || value.Cells.Empty()) {
			return PixelRect{}, CellRect{}, false, false
		}
		return value.Pixels, value.Cells, hasPixels, hasCells
	case *Viewport:
		if value == nil {
			return PixelRect{}, CellRect{}, false, false
		}
		return viewportParts(*value)
	default:
		return PixelRect{}, CellRect{}, false, false
	}
}

func visiblePlacement(placement PlacementView, pixels PixelRect, cells CellRect, hasPixels, hasCells bool) (VisibleFragment, bool) {
	destination := placement.destination
	if hasPixels {
		var ok bool
		destination, ok = destination.Intersect(pixels)
		if !ok {
			return VisibleFragment{}, false
		}
	}
	var visibleCells CellRect
	if placement.cells.Valid() && !placement.cells.Empty() {
		visibleCells = placement.cells
		if hasCells {
			var ok bool
			visibleCells, ok = visibleCells.Intersect(cells)
			if !ok {
				return VisibleFragment{}, false
			}
			cellPixels, ok := mapSubRectCellToPixel(placement.cells, placement.destination, visibleCells)
			if !ok {
				return VisibleFragment{}, false
			}
			var pixelOK bool
			destination, pixelOK = destination.Intersect(cellPixels)
			if !pixelOK {
				return VisibleFragment{}, false
			}
		}
		if hasPixels {
			var ok bool
			visibleCells, ok = mapSubRectPixelToCell(placement.destination, placement.cells, destination)
			if !ok {
				return VisibleFragment{}, false
			}
			if hasCells {
				visibleCells, ok = visibleCells.Intersect(cells)
				if !ok {
					return VisibleFragment{}, false
				}
			}
		}
	} else if hasCells {
		return VisibleFragment{}, false
	}
	source, ok := mapSubRect(placement.destination, placement.source, destination)
	if !ok {
		return VisibleFragment{}, false
	}
	return VisibleFragment{PlacementID: placement.id, AssetID: placement.asset, Source: source, Destination: destination, Cells: visibleCells}, true
}

// mapSubRect maps a non-empty sub-rectangle from one rectangle's coordinate
// space into another rectangle's coordinate space. It uses exact 128+-bit
// arithmetic through math/big so products cannot wrap before division.
func mapSubRect(from, to, sub PixelRect) (PixelRect, bool) {
	if !from.Valid() || !to.Valid() || !sub.Valid() || from.Empty() || to.Empty() || sub.Empty() || !containsRect(from, sub) {
		return PixelRect{}, false
	}
	x, okX := mapRange(from.X, from.Width, to.X, to.Width, sub.X, mustRight(sub.X, sub.Width))
	y, okY := mapRange(from.Y, from.Height, to.Y, to.Height, sub.Y, mustRight(sub.Y, sub.Height))
	if !okX || !okY || x[1] <= x[0] || y[1] <= y[0] {
		return PixelRect{}, false
	}
	return PixelRect{X: x[0], Y: y[0], Width: x[1] - x[0], Height: y[1] - y[0]}, true
}

func mapSubRectCellToPixel(from CellRect, to PixelRect, sub CellRect) (PixelRect, bool) {
	if !from.Valid() || !to.Valid() || !sub.Valid() || from.Empty() || to.Empty() || sub.Empty() || !containsCellRect(from, sub) {
		return PixelRect{}, false
	}
	x, okX := mapRange(from.X, from.Width, to.X, to.Width, sub.X, mustRight(sub.X, sub.Width))
	y, okY := mapRange(from.Y, from.Height, to.Y, to.Height, sub.Y, mustRight(sub.Y, sub.Height))
	if !okX || !okY || x[1] <= x[0] || y[1] <= y[0] {
		return PixelRect{}, false
	}
	return PixelRect{X: x[0], Y: y[0], Width: x[1] - x[0], Height: y[1] - y[0]}, true
}

func mapSubRectPixelToCell(from PixelRect, to CellRect, sub PixelRect) (CellRect, bool) {
	if !from.Valid() || !to.Valid() || !sub.Valid() || from.Empty() || to.Empty() || sub.Empty() || !containsRect(from, sub) {
		return CellRect{}, false
	}
	x, okX := mapRange(from.X, from.Width, to.X, to.Width, sub.X, mustRight(sub.X, sub.Width))
	y, okY := mapRange(from.Y, from.Height, to.Y, to.Height, sub.Y, mustRight(sub.Y, sub.Height))
	if !okX || !okY || x[1] <= x[0] || y[1] <= y[0] {
		return CellRect{}, false
	}
	return CellRect{X: x[0], Y: y[0], Width: x[1] - x[0], Height: y[1] - y[0]}, true
}

func containsCellRect(outer, inner CellRect) bool {
	right, okRight := outer.Right()
	innerRight, okInnerRight := inner.Right()
	bottom, okBottom := outer.Bottom()
	innerBottom, okInnerBottom := inner.Bottom()
	return okRight && okInnerRight && okBottom && okInnerBottom && inner.X >= outer.X && innerRight <= right && inner.Y >= outer.Y && innerBottom <= bottom
}

func mustRight(start, width int64) int64 {
	right, _ := checkedEdge(start, width)
	return right
}

func mapRange(fromStart, fromSize, toStart, toSize, clipStart, clipEnd int64) ([2]int64, bool) {
	if fromSize <= 0 || toSize <= 0 || clipStart < fromStart || clipEnd < clipStart {
		return [2]int64{}, false
	}
	fromEnd, ok := checkedEdge(fromStart, fromSize)
	if !ok || clipEnd > fromEnd {
		return [2]int64{}, false
	}
	startOffset := clipStart - fromStart
	endOffset := clipEnd - fromStart
	startDelta, ok := ratio(startOffset, toSize, fromSize, false)
	if !ok {
		return [2]int64{}, false
	}
	endDelta, ok := ratio(endOffset, toSize, fromSize, true)
	if !ok {
		return [2]int64{}, false
	}
	start, ok := checkedAdd(toStart, startDelta)
	if !ok {
		return [2]int64{}, false
	}
	end, ok := checkedAdd(toStart, endDelta)
	if !ok || end <= start {
		return [2]int64{}, false
	}
	return [2]int64{start, end}, true
}

func ratio(offset, numerator, denominator int64, ceil bool) (int64, bool) {
	if offset < 0 || numerator <= 0 || denominator <= 0 {
		return 0, false
	}
	product := new(big.Int).Mul(big.NewInt(offset), big.NewInt(numerator))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(product, big.NewInt(denominator), remainder)
	if ceil && remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() || quotient.Sign() < 0 {
		return 0, false
	}
	return quotient.Int64(), true
}

func checkedAdd(a, b int64) (int64, bool) {
	if b > 0 && a > maxInt64-b {
		return 0, false
	}
	if b < 0 && a < minInt64-b {
		return 0, false
	}
	return a + b, true
}
