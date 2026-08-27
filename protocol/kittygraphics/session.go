package kittygraphics

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"image/png"
	"io"
	"math"
	"strconv"

	"github.com/bnema/vev-vt/graphics"
)

type placementKey struct {
	imageID     uint64
	placementID uint64
}

type placementOrigin struct {
	x, y uint64
	set  bool
}

type upload struct {
	controls Controls
	imageID  uint64
	payload  []byte
	chunks   uint64
	display  bool
	origin   placementOrigin
}

// MutationKind describes a scene mutation performed by a session.
type MutationKind uint8

const (
	MutationUpload MutationKind = iota + 1
	MutationPlacement
	MutationDeleteImage
	MutationDeletePlacement
	MutationClear
	MutationDeletePlacements
)

// Mutation records the stable protocol and opaque scene IDs involved in one
// accepted command.
type Mutation struct {
	Kind             MutationKind
	ImageID          uint64
	ChildID          uint64
	AssetID          graphics.AssetID
	PlacementID      uint64
	ScenePlacementID graphics.PlacementID
}

// Result contains all output from one or more commands in a Feed call.
type Result struct {
	Events    []Event
	Responses [][]byte
	Mutations []Mutation
}

// Bytes returns response bytes in stable command order. The returned slice is
// newly allocated and may be modified by the caller.
func (r Result) Bytes() []byte {
	var n int
	for _, response := range r.Responses {
		n += len(response)
	}
	out := make([]byte, 0, n)
	for _, response := range r.Responses {
		out = append(out, response...)
	}
	return out
}

// Response is the wire representation of a stable Kitty response.
type Response struct {
	ImageID uint64
	OK      bool
	Code    string
	Bytes   []byte
}

const responsePrefix = "\x1b_G"
const responseSuffix = "\x1b\\"

// MakeResponse constructs the canonical Kitty response used by Session.
func MakeResponse(imageID uint64, code string) []byte {
	if code == "" {
		code = "OK"
	}
	return []byte(responsePrefix + "i=" + strconv.FormatUint(imageID, 10) + ";" + code + responseSuffix)
}

// Feed parses and applies arbitrary bytes. Ordinary text is returned as
// EventText; it is never interpreted, discarded, or passed through graphics
// scene mutation. A malformed command is reported after all complete events
// in the same input have been handled.
func (s *Session) Feed(data []byte) (Result, error) {
	if s == nil {
		return Result{}, ErrNoScene
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var result Result
	for _, event := range s.parser.Feed(data) {
		result.Events = append(result.Events, event)
		if event.Kind == EventError {
			s.upload = nil
			continue
		}
		if event.Kind != EventCommand {
			continue
		}
		responses, mutation, err := s.apply(event.Command)
		if len(responses) != 0 {
			result.Responses = append(result.Responses, responses...)
		}
		if mutation != nil {
			result.Mutations = append(result.Mutations, *mutation)
		}
		if err != nil {
			// Keep the error attached to the event as well as returning it
			// below. This makes mixed text/graphics feeds inspectable without
			// requiring callers to reconstruct command order.
			result.Events[len(result.Events)-1].Kind = EventError
			result.Events[len(result.Events)-1].Err = err
		}
	}
	var first error
	for _, event := range result.Events {
		if event.Kind == EventError && event.Err != nil {
			first = event.Err
			break
		}
	}
	return result, first
}

// Write is an adapter-oriented alias for Feed.
func (s *Session) Write(data []byte) (Result, error) { return s.Feed(data) }

// Handle is an adapter-oriented alias for Feed.
func (s *Session) Handle(data []byte) (Result, error) { return s.Feed(data) }

// Finish flushes an incomplete parser sequence. It never mutates the scene.
func (s *Session) Finish() (Result, error) {
	if s == nil {
		return Result{}, ErrNoScene
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := Result{Events: s.parser.Finish()}
	s.upload = nil
	if len(result.Events) != 0 {
		return result, result.Events[0].Err
	}
	return result, nil
}

// Process applies one already parsed command. It is useful for callers that
// have their own stream framing but want this package's strict adapter.
func (s *Session) Process(command Command) (Result, error) {
	if s == nil {
		return Result{}, ErrNoScene
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	responses, mutation, err := s.apply(command)
	result := Result{Responses: responses}
	if mutation != nil {
		result.Mutations = []Mutation{*mutation}
	}
	return result, err
}

// SetPendingPlacement supplies the cursor-relative pixel origin for the next
// placement. The origin is adapter context, not a Kitty control field: Kitty
// X/Y remain pixel offsets within the cursor cell.
func (s *Session) SetPendingPlacement(x, y uint64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.origin = placementOrigin{x: x, y: y, set: true}
	if s.upload != nil {
		s.upload.origin = s.origin
	}
}

// AbortPendingUpload discards an in-flight chunked upload without changing
// committed images or placements. Screen-level full clears use it to ensure a
// continuation arriving after the clear cannot publish stale graphics.
func (s *Session) AbortPendingUpload() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upload = nil
}

func (s *Session) apply(command Command) ([][]byte, *Mutation, error) {
	c := command.Controls
	action := c.Action
	if !c.HasAction {
		action = ActionTransmit
	}
	if c.HasCompression && c.Compression != CompressionZlib {
		return s.failure(c, controlsImageID(c), ErrUnsupported)
	}
	if c.HasTransmission && c.Transmission != TransmissionDirect {
		return s.failure(c, controlsImageID(c), ErrUnsupported)
	}
	if action == ActionDelete {
		s.upload = nil
	} else if s.upload != nil && action != ActionTransmit && action != ActionTransmitDisplay {
		return s.failure(c, controlsImageID(c), ErrInterleavedUpload)
	}
	switch action {
	case ActionTransmit, ActionTransmitDisplay:
		return s.transmit(command, action == ActionTransmitDisplay)
	case ActionPut:
		if len(command.Payload) != 0 {
			return s.failure(c, controlsImageID(c), ErrInvalidCommand)
		}
		return s.put(c)
	case ActionQuery:
		return s.query(command)
	case ActionDelete:
		if len(command.Payload) != 0 {
			return s.failure(c, controlsImageID(c), ErrInvalidCommand)
		}
		return s.delete(c)
	case ActionFrame, ActionCompose:
		return s.failure(c, controlsImageID(c), ErrUnsupported)
	default:
		return s.failure(c, controlsImageID(c), ErrUnknownAction)
	}
}

func (s *Session) transmit(command Command, display bool) ([][]byte, *Mutation, error) {
	c := command.Controls
	imageID := c.ImageID
	if !c.HasImageID || imageID == 0 {
		imageID = 0
		if c.HasImageNumber {
			imageID = s.nextImageID()
		}
	}
	if s.upload != nil {
		if !continuationControlsAllowed(c, s.upload.controls) {
			return s.failure(c, responseImageID(c), ErrInterleavedUpload)
		}
		if c.HasImageID && c.ImageID != s.upload.imageID {
			return s.failure(c, c.ImageID, ErrInterleavedUpload)
		}
		if c.HasImageNumber && (!s.upload.controls.HasImageNumber || c.ImageNumber != s.upload.controls.ImageNumber) {
			return s.failure(c, responseImageID(c), ErrInterleavedUpload)
		}
		imageID = s.upload.imageID
		if s.upload.chunks >= s.limits.MaxChunks {
			return s.abortUpload(c, imageID, ErrTooManyChunks)
		}
		if uint64(len(s.upload.payload))+uint64(len(command.Payload)) > s.limits.MaxUploadBytes {
			return s.abortUpload(c, imageID, ErrPayloadTooLarge)
		}
		s.upload.payload = append(s.upload.payload, command.Payload...)
		s.upload.chunks++
		s.upload.controls = mergeControls(s.upload.controls, c)
		if c.HasMore && c.More == 1 {
			return nil, nil, nil
		}
		complete := *s.upload
		s.upload = nil
		complete.controls = mergeControls(complete.controls, c)
		return s.commitUpload(complete, complete.display)
	}

	if uint64(len(command.Payload)) > s.limits.MaxUploadBytes {
		return s.failure(c, imageID, ErrPayloadTooLarge)
	}
	if c.HasMore && c.More == 1 {
		s.upload = &upload{
			controls: c,
			imageID:  imageID,
			payload:  append([]byte(nil), command.Payload...),
			chunks:   1,
			display:  display,
			origin:   s.origin,
		}
		return nil, nil, nil
	}
	return s.commitUpload(upload{controls: c, imageID: imageID, payload: append([]byte(nil), command.Payload...), chunks: 1, display: display, origin: s.origin}, display)
}

func (s *Session) commitUpload(value upload, display bool) ([][]byte, *Mutation, error) {
	if uint64(len(value.payload)) > s.limits.MaxPayloadBytes || uint64(len(value.payload)) > s.limits.MaxUploadBytes {
		return s.failure(value.controls, value.imageID, ErrPayloadTooLarge)
	}
	decoded, err := decodeImagePayload(value.payload, value.controls, s.limits)
	if err != nil {
		return s.failure(value.controls, value.imageID, err)
	}
	format := value.controls.Format
	if !value.controls.HasFormat {
		format = FormatRGBA
	}
	width, height, err := imageGeometry(decoded, format, value.controls, s.limits)
	if err != nil {
		return s.failure(value.controls, value.imageID, err)
	}
	if s.scene == nil {
		return s.failure(value.controls, value.imageID, ErrNoScene)
	}
	persistent := value.controls.HasImageID && value.controls.ImageID != 0 || value.controls.HasImageNumber
	old, replacing := s.images[value.imageID]
	if !persistent {
		replacing = false
	}
	if !replacing && s.scene.Usage().Assets >= s.limits.MaxImages {
		return s.failure(value.controls, value.imageID, graphics.ErrTooManyAssets)
	}
	if replacing && display {
		// Validate the prospective placement before replacing the asset. A
		// rejected T command must leave both the old asset and its placements
		// available for subsequent use.
		if err := s.validateReplacementPlacement(value.controls, old, width, height); err != nil {
			return s.failure(value.controls, value.imageID, err)
		}
	}
	var assetID graphics.AssetID
	if replacing {
		// Scene replacement is copy-on-write: a rejected new asset leaves the
		// old asset and every placement that references it untouched.
		assetID, err = s.scene.ReplaceAsset(old, graphics.AssetBlob{
			Encoded: decoded,
			Format:  assetFormat(format),
			Width:   width,
			Height:  height,
		})
	} else {
		assetID, err = s.scene.AddAsset(graphics.AssetBlob{
			Encoded: decoded,
			Format:  assetFormat(format),
			Width:   width,
			Height:  height,
		})
	}
	if err != nil {
		return s.failure(value.controls, value.imageID, err)
	}
	if replacing {
		s.removeMappingsForAsset(old)
	}
	if persistent {
		s.images[value.imageID] = assetID
	}
	if value.controls.HasImageNumber {
		s.imageNumbers[value.controls.ImageNumber] = value.imageID
	}
	childID := s.nextChild
	s.nextChild++
	s.children[childID] = Child{ID: childID, ImageID: value.imageID, AssetID: assetID}
	mutation := &Mutation{Kind: MutationUpload, ImageID: value.imageID, ChildID: childID, AssetID: assetID}
	if display {
		responses, placementMutation, placeErr := s.place(value.controls, value.imageID, assetID, value.origin)
		if placementMutation != nil {
			mutation.Kind = MutationPlacement
			mutation.PlacementID = placementMutation.PlacementID
			mutation.ScenePlacementID = placementMutation.ScenePlacementID
		}
		if placeErr != nil {
			// T (transmit and display) is one protocol operation. Do not
			// leave an uploaded asset behind when its placement is rejected.
			_ = s.scene.RemoveAssetCascade(assetID)
			if persistent {
				delete(s.images, value.imageID)
			}
			if value.controls.HasImageNumber && s.imageNumbers[value.controls.ImageNumber] == value.imageID {
				delete(s.imageNumbers, value.controls.ImageNumber)
			}
			delete(s.children, childID)
			failureResponses, _, failureErr := s.failure(value.controls, value.imageID, placeErr)
			return failureResponses, nil, failureErr
		}
		return s.success(value.controls, value.imageID, responses...), mutation, nil
	}
	return s.success(value.controls, value.imageID), mutation, nil
}

func decodeImagePayload(payload []byte, controls Controls, limits Limits) ([]byte, error) {
	decoded, err := DecodeBase64(payload)
	if err != nil {
		return nil, err
	}
	if !controls.HasCompression {
		return decoded, nil
	}
	if controls.Compression != CompressionZlib {
		return nil, ErrUnsupported
	}
	zr, err := zlib.NewReader(bytes.NewReader(decoded))
	if err != nil {
		return nil, fmt.Errorf("%w: zlib: %v", ErrInvalidCommand, err)
	}
	defer func() { _ = zr.Close() }()
	limit := limits.MaxDecodedBytes
	if limit >= math.MaxInt64 {
		limit = math.MaxInt64 - 1
	}
	decompressed, err := io.ReadAll(io.LimitReader(zr, int64(limit)+1))
	if err != nil {
		return nil, fmt.Errorf("%w: zlib: %v", ErrInvalidCommand, err)
	}
	if uint64(len(decompressed)) > limits.MaxDecodedBytes {
		return nil, ErrPayloadTooLarge
	}
	return decompressed, nil
}

func assetFormat(format Format) graphics.AssetFormat {
	switch format {
	case FormatRGB:
		return graphics.AssetFormatRGB
	case FormatRGBA:
		return graphics.AssetFormatRGBA
	case FormatPNG:
		return graphics.AssetFormatPNG
	default:
		return graphics.AssetFormatUnknown
	}
}

func imageGeometry(data []byte, format Format, c Controls, limits Limits) (int64, int64, error) {
	if format == FormatPNG {
		config, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return 0, 0, fmt.Errorf("%w: %v", ErrInvalidPNG, err)
		}
		if config.Width <= 0 || config.Height <= 0 {
			return 0, 0, ErrInvalidPNG
		}
		if uint64(config.Width) > limits.MaxDimension || uint64(config.Height) > limits.MaxDimension {
			return 0, 0, ErrPayloadTooLarge
		}
		pixels, ok := checkedProduct(uint64(config.Width), uint64(config.Height))
		decodedBytes, bytesOK := checkedProduct(pixels, 4)
		if !ok || !bytesOK || pixels > limits.MaxDecodedPixels || decodedBytes > limits.MaxDecodedBytes {
			return 0, 0, ErrPayloadTooLarge
		}
		if c.HasWidth && c.Width != uint64(config.Width) || c.HasHeight && c.Height != uint64(config.Height) {
			return 0, 0, fmt.Errorf("%w: PNG dimensions do not match controls", ErrInvalidPNG)
		}
		// Decode, rather than only DecodeConfig, so malformed/truncated IDAT
		// data and CRC failures are rejected by the standard library.
		if _, err := png.Decode(bytes.NewReader(data)); err != nil {
			return 0, 0, fmt.Errorf("%w: %v", ErrInvalidPNG, err)
		}
		return int64(config.Width), int64(config.Height), nil
	}
	if !format.Valid() {
		return 0, 0, ErrUnsupported
	}
	if !c.HasWidth || !c.HasHeight || c.Width == 0 || c.Height == 0 {
		return 0, 0, fmt.Errorf("%w: raw format needs s and v", ErrInvalidCommand)
	}
	if c.Width > limits.MaxDimension || c.Height > limits.MaxDimension {
		return 0, 0, ErrPayloadTooLarge
	}
	pixels, ok := checkedProduct(c.Width, c.Height)
	if !ok || pixels > limits.MaxDecodedPixels {
		return 0, 0, ErrPayloadTooLarge
	}
	channels := uint64(3)
	if format == FormatRGBA {
		channels = 4
	}
	rowBytes, ok := checkedProduct(c.Height, channels)
	if !ok {
		return 0, 0, ErrPayloadTooLarge
	}
	expected, ok := checkedProduct(c.Width, rowBytes)
	if !ok || expected > limits.MaxDecodedBytes {
		return 0, 0, ErrPayloadTooLarge
	}
	if uint64(len(data)) != expected {
		return 0, 0, fmt.Errorf("%w: raw payload has %d bytes, want %d", ErrInvalidCommand, len(data), expected)
	}
	return int64(c.Width), int64(c.Height), nil
}

func (s *Session) put(c Controls) ([][]byte, *Mutation, error) {
	imageID, assetID, ok := s.resolveImage(c)
	if !ok {
		return s.failure(c, responseImageID(c), ErrImageNotFound)
	}
	responses, mutation, err := s.place(c, imageID, assetID, s.origin)
	if err != nil {
		failureResponses, _, failureErr := s.failure(c, imageID, err)
		return failureResponses, nil, failureErr
	}
	return s.success(c, imageID, responses...), mutation, nil
}

func (s *Session) validateReplacementPlacement(c Controls, old graphics.AssetID, width, height int64) error {
	if _, err := placementSpec(c, old, width, height, s.origin); err != nil {
		return err
	}
	snapshot := s.scene.Snapshot()
	removedScene := uint64(0)
	for _, placement := range snapshot.Placements() {
		if placement.AssetID() == old {
			removedScene++
		}
	}
	removedSession := uint64(0)
	for _, mapped := range s.placements {
		placement, ok := snapshot.Placement(mapped)
		if !ok || placement.AssetID() == old {
			removedSession++
		}
	}
	projectedSession := uint64(len(s.placements)) - removedSession
	projectedScene := snapshot.Usage().Placements - removedScene
	needsSlot := true
	if c.HasPlacementID && c.PlacementID != 0 {
		if mapped, ok := s.placements[placementKey{imageID: c.ImageID, placementID: c.PlacementID}]; ok {
			if placement, ok := snapshot.Placement(mapped); ok && placement.AssetID() != old {
				needsSlot = false
			}
		}
	}
	if needsSlot && projectedSession >= s.limits.MaxPlacements {
		return graphics.ErrTooManyPlacements
	}
	if needsSlot && projectedScene >= s.scene.Limits().MaxPlacements {
		return graphics.ErrTooManyPlacements
	}
	if s.scene.Generation() >= ^uint64(0)-1 {
		return graphics.ErrGenerationOverflow
	}
	return nil
}

func placementSpec(c Controls, assetID graphics.AssetID, width, height int64, origin placementOrigin) (graphics.PlacementSpec, error) {
	source := graphics.PixelRect{Width: width, Height: height}
	var err error
	if source.X, err = checkedCoordinate(c.X, c.HasX); err != nil {
		return graphics.PlacementSpec{}, err
	}
	if source.Y, err = checkedCoordinate(c.Y, c.HasY); err != nil {
		return graphics.PlacementSpec{}, err
	}
	if source.Width, err = checkedDimension(c.SourceWidth, c.HasSourceWidth, width); err != nil {
		return graphics.PlacementSpec{}, err
	}
	if source.Height, err = checkedDimension(c.SourceHeight, c.HasSourceHeight, height); err != nil {
		return graphics.PlacementSpec{}, err
	}
	offsetX, err := checkedCoordinate(c.SourceX, c.HasSourceX)
	if err != nil {
		return graphics.PlacementSpec{}, err
	}
	offsetY, err := checkedCoordinate(c.SourceY, c.HasSourceY)
	if err != nil {
		return graphics.PlacementSpec{}, err
	}
	destination := graphics.PixelRect{X: offsetX, Y: offsetY, Width: source.Width, Height: source.Height}
	if origin.set {
		if origin.x > math.MaxInt64 || origin.y > math.MaxInt64 || destination.X > math.MaxInt64-int64(origin.x) || destination.Y > math.MaxInt64-int64(origin.y) {
			return graphics.PlacementSpec{}, ErrIntegerOverflow
		}
		destination.X += int64(origin.x)
		destination.Y += int64(origin.y)
	}
	if !source.Valid() || !destination.Valid() {
		return graphics.PlacementSpec{}, graphics.ErrInvalidRect
	}
	assetBounds := graphics.PixelRect{Width: width, Height: height}
	clipped, ok := assetBounds.Intersect(source)
	if !ok || clipped != source {
		return graphics.PlacementSpec{}, graphics.ErrInvalidPlacement
	}
	var cells graphics.CellRect
	if c.HasColumns || c.HasRows {
		if !c.HasColumns || !c.HasRows || c.Columns == 0 || c.Rows == 0 || c.Columns > math.MaxInt64 || c.Rows > math.MaxInt64 {
			return graphics.PlacementSpec{}, fmt.Errorf("%w: c/r", ErrInvalidCommand)
		}
		cells = graphics.CellRect{X: signedCoordinateInt(c.CellOffsetX, c.HasCellOffsetX), Y: signedCoordinateInt(c.CellOffsetY, c.HasCellOffsetY), Width: int64(c.Columns), Height: int64(c.Rows)}
		if !cells.Valid() {
			return graphics.PlacementSpec{}, graphics.ErrInvalidRect
		}
	}
	spec := graphics.PlacementSpec{Asset: assetID, Source: source, Destination: destination, Cells: cells, HasCells: c.HasColumns || c.HasRows}
	if c.HasLayer {
		spec.Layer = c.Layer
		spec.HasLayer = true
	}
	return spec, nil
}

func (s *Session) place(c Controls, imageID uint64, assetID graphics.AssetID, origin placementOrigin) ([][]byte, *Mutation, error) {
	if c.HasParent || c.HasCellOffsetX || c.HasCellOffsetY {
		return nil, nil, ErrUnsupported
	}
	if s.scene == nil {
		return nil, nil, ErrNoScene
	}
	asset, ok := s.scene.Snapshot().Asset(assetID)
	if !ok {
		return nil, nil, ErrImageNotFound
	}
	spec, err := placementSpec(c, assetID, asset.Width(), asset.Height(), origin)
	if err != nil {
		return nil, nil, err
	}
	placementID := c.PlacementID
	if imageID == 0 || placementID == 0 {
		var ok bool
		placementID, ok = s.takePlacementID(imageID)
		if !ok {
			return nil, nil, graphics.ErrIdentifierOverflow
		}
	}
	key := placementKey{imageID: imageID, placementID: placementID}
	if existing, ok := s.placements[key]; ok {
		if err := s.scene.UpdatePlacement(existing, spec); err != nil {
			return nil, nil, err
		}
		return nil, &Mutation{Kind: MutationPlacement, ImageID: imageID, AssetID: assetID, PlacementID: placementID, ScenePlacementID: existing}, nil
	}
	if uint64(len(s.placements)) >= s.limits.MaxPlacements {
		return nil, nil, graphics.ErrTooManyPlacements
	}
	sceneID, err := s.scene.Place(spec)
	if err != nil {
		return nil, nil, err
	}
	s.placements[key] = sceneID
	return nil, &Mutation{Kind: MutationPlacement, ImageID: imageID, AssetID: assetID, PlacementID: placementID, ScenePlacementID: sceneID}, nil
}

func (s *Session) query(command Command) ([][]byte, *Mutation, error) {
	c := command.Controls
	if len(command.Payload) == 0 {
		return s.failure(c, responseImageID(c), ErrInvalidCommand)
	}
	decoded, err := decodeImagePayload(command.Payload, c, s.limits)
	if err != nil {
		return s.failure(c, responseImageID(c), err)
	}
	format := c.Format
	if !c.HasFormat {
		format = FormatRGBA
	}
	if _, _, err := imageGeometry(decoded, format, c, s.limits); err != nil {
		return s.failure(c, responseImageID(c), err)
	}
	return s.success(c, responseImageID(c)), nil, nil
}

func (s *Session) delete(c Controls) ([][]byte, *Mutation, error) {
	target := c.Delete
	if !c.HasDelete {
		target = DeleteAll
	}
	id, asset, selected := s.resolveImage(c)
	responseID := responseImageID(c)
	switch target {
	case DeleteImage:
		if !selected || s.scene == nil {
			return s.failure(c, responseID, ErrImageNotFound)
		}
		keys := s.placementKeysForAsset(asset)
		if c.HasPlacementID && c.PlacementID != 0 {
			keys = []placementKey{{imageID: id, placementID: c.PlacementID}}
		}
		if err := s.removePlacements(keys); err != nil {
			return s.failure(c, id, err)
		}
		return s.success(c, id), &Mutation{Kind: MutationDeletePlacements, ImageID: id, AssetID: asset}, nil
	case DeleteImageNumber:
		if !selected {
			return s.failure(c, responseID, ErrImageNotFound)
		}
		if s.scene == nil {
			return s.failure(c, id, ErrNoScene)
		}
		s.removeMappingsForAsset(asset)
		if err := s.scene.RemoveAssetCascade(asset); err != nil {
			return s.failure(c, id, err)
		}
		return s.success(c, id), &Mutation{Kind: MutationDeleteImage, ImageID: id, AssetID: asset}, nil
	case DeleteAll:
		if s.scene == nil {
			return s.failure(c, responseID, ErrNoScene)
		}
		placementIDs := make([]placementKey, 0, len(s.placements))
		for placementID := range s.placements {
			placementIDs = append(placementIDs, placementID)
		}
		if err := s.removePlacements(placementIDs); err != nil {
			return s.failure(c, responseID, err)
		}
		return s.success(c, responseID), &Mutation{Kind: MutationDeletePlacements}, nil
	case DeleteCell, DeleteCellAll:
		// Kitty d=p/P deletes by cell intersection, not by placement ID or
		// all placements. Those selectors remain unsupported until the scene
		// tracks terminal cell anchors.
		return s.failure(c, responseID, ErrUnsupported)
	case DeleteAllImages:
		if s.scene == nil {
			return s.failure(c, responseID, ErrNoScene)
		}
		if err := s.scene.Clear(); err != nil {
			return s.failure(c, responseID, err)
		}
		s.images = make(map[uint64]graphics.AssetID)
		s.imageNumbers = make(map[uint64]uint64)
		s.placements = make(map[placementKey]graphics.PlacementID)
		s.children = make(map[uint64]Child)
		return s.success(c, responseID), &Mutation{Kind: MutationClear}, nil
	default:
		return s.failure(c, responseID, ErrUnsupported)
	}
}

func (s *Session) placementKeysForAsset(asset graphics.AssetID) []placementKey {
	if s.scene == nil {
		return nil
	}
	snapshot := s.scene.Snapshot()
	keys := make([]placementKey, 0)
	for key, mapped := range s.placements {
		placement, ok := snapshot.Placement(mapped)
		if ok && placement.AssetID() == asset {
			keys = append(keys, key)
		}
	}
	return keys
}

func (s *Session) removePlacements(ids []placementKey) error {
	if len(ids) == 0 {
		return nil
	}
	operations := make([]graphics.Operation, 0, len(ids))
	for _, id := range ids {
		mapped, ok := s.placements[id]
		if !ok {
			continue
		}
		operations = append(operations, graphics.Operation{
			Kind:        graphics.OperationRemovePlacement,
			PlacementID: mapped,
		})
	}
	if len(operations) == 0 {
		return nil
	}
	if err := s.scene.ApplyOperations(operations); err != nil {
		return err
	}
	for _, id := range ids {
		delete(s.placements, id)
	}
	return nil
}

func (s *Session) removeMappingsForAsset(asset graphics.AssetID) {
	for imageID, mapped := range s.images {
		if mapped == asset {
			delete(s.images, imageID)
		}
	}
	for imageNumber, imageID := range s.imageNumbers {
		if mapped, ok := s.images[imageID]; !ok || mapped == asset {
			delete(s.imageNumbers, imageNumber)
		}
	}
	if s.scene != nil {
		snapshot := s.scene.Snapshot()
		for placementID, mapped := range s.placements {
			placement, ok := snapshot.Placement(mapped)
			if !ok || placement.AssetID() == asset {
				delete(s.placements, placementID)
			}
		}
	}
	for childID, child := range s.children {
		if child.AssetID == asset {
			delete(s.children, childID)
		}
	}
}

func continuationControlsAllowed(current, initial Controls) bool {
	if current.HasAction {
		initialAction := initial.Action
		if !initial.HasAction {
			initialAction = ActionTransmit
		}
		if current.Action != initialAction {
			return false
		}
	}
	return !current.HasCompression && !current.HasFormat && !current.HasImageID &&
		!current.HasImageNumber && !current.HasWidth && !current.HasHeight &&
		!current.HasColumns && !current.HasRows && !current.HasX && !current.HasY &&
		!current.HasSourceWidth && !current.HasSourceHeight && !current.HasLayer &&
		!current.HasPlacementID && !current.HasDelete && !current.HasTransmission &&
		!current.HasCursor && !current.HasSourceX && !current.HasSourceY &&
		!current.HasCellOffsetX && !current.HasCellOffsetY && !current.HasParent
}

func (s *Session) resolveImage(c Controls) (uint64, graphics.AssetID, bool) {
	if c.HasImageID {
		asset, ok := s.images[c.ImageID]
		return c.ImageID, asset, ok
	}
	if c.HasImageNumber {
		imageID, ok := s.imageNumbers[c.ImageNumber]
		if !ok {
			return 0, graphics.AssetID{}, false
		}
		asset, ok := s.images[imageID]
		return imageID, asset, ok
	}
	return 0, graphics.AssetID{}, false
}

func responseImageID(c Controls) uint64 {
	if c.HasImageID {
		return c.ImageID
	}
	return 0
}

func controlsImageID(c Controls) uint64 { return responseImageID(c) }

func (s *Session) nextImageID() uint64 {
	for id := uint64(1); ; id++ {
		if _, ok := s.images[id]; !ok {
			return id
		}
	}
}

func (s *Session) takePlacementID(imageID uint64) (uint64, bool) {
	for s.nextPlacement != 0 && s.nextPlacement <= MaxKittyID {
		id := s.nextPlacement
		if s.nextPlacement == MaxKittyID {
			s.nextPlacement = 0
		} else {
			s.nextPlacement++
		}
		if _, exists := s.placements[placementKey{imageID: imageID, placementID: id}]; !exists {
			return id, true
		}
	}
	return 0, false
}

func checkedProduct(left, right uint64) (uint64, bool) {
	if left != 0 && right > math.MaxUint64/left {
		return 0, false
	}
	return left * right, true
}

func checkedCoordinate(n uint64, present bool) (int64, error) {
	if !present {
		return 0, nil
	}
	if n > math.MaxInt64 {
		return 0, ErrIntegerOverflow
	}
	return int64(n), nil
}

func checkedDimension(n uint64, present bool, fallback int64) (int64, error) {
	if !present {
		return fallback, nil
	}
	if n > math.MaxInt64 {
		return 0, ErrIntegerOverflow
	}
	return int64(n), nil
}

func signedCoordinateInt(n int64, present bool) int64 {
	if !present {
		return 0
	}
	return n
}

func (s *Session) success(c Controls, imageID uint64, prior ...[]byte) [][]byte {
	responses := prior
	if responseRequested(c) && c.Quiet != QuietSuccess && c.Quiet != QuietAll {
		response := makeControlResponse(c, imageID, "OK")
		if uint64(len(response)) <= s.limits.MaxResponseBytes {
			responses = append(responses, response)
		}
	}
	return responses
}

// responseRequested matches Kitty's response routing: commands without a
// non-zero image ID or image number are anonymous and never receive replies.
func responseRequested(c Controls) bool {
	return c.HasImageID && c.ImageID != 0 || c.HasImageNumber && c.ImageNumber != 0
}

func makeControlResponse(c Controls, imageID uint64, code string) []byte {
	if !c.HasImageNumber {
		return MakeResponse(imageID, code)
	}
	if code == "" {
		code = "OK"
	}
	return []byte(responsePrefix + "i=" + strconv.FormatUint(imageID, 10) + ",I=" + strconv.FormatUint(c.ImageNumber, 10) + ";" + code + responseSuffix)
}

func (s *Session) failure(c Controls, imageID uint64, err error) ([][]byte, *Mutation, error) {
	if err == nil {
		err = ErrInvalidCommand
	}
	if responseRequested(c) && c.Quiet != QuietAll {
		code := errorCode(err)
		response := makeControlResponse(c, imageID, code)
		if uint64(len(response)) <= s.limits.MaxResponseBytes {
			return [][]byte{response}, nil, err
		}
	}
	return nil, nil, err
}

func (s *Session) abortUpload(c Controls, imageID uint64, err error) ([][]byte, *Mutation, error) {
	s.upload = nil
	return s.failure(c, imageID, err)
}

func errorCode(err error) string {
	switch {
	case err == nil:
		return "OK"
	case errorsIs(err, ErrImageNotFound), errorsIs(err, ErrPlacementNotFound), errorsIs(err, graphics.ErrAssetNotFound), errorsIs(err, graphics.ErrPlacementNotFound):
		return "ENOENT"
	case errorsIs(err, ErrPayloadTooLarge), errorsIs(err, ErrAPCTooLarge), errorsIs(err, graphics.ErrEncodedBudget), errorsIs(err, graphics.ErrDecodedPixelBudget):
		return "E2BIG"
	case errorsIs(err, ErrUnsupported), errorsIs(err, ErrUnknownAction):
		return "ENOTSUP"
	default:
		return "EINVAL"
	}
}

// errorsIs is kept local to make error-code selection explicit and avoid
// leaking any protocol error implementation details in the public API.
func errorsIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func mergeControls(base, override Controls) Controls {
	out := base
	if override.HasAction {
		out.Action, out.HasAction = override.Action, true
	}
	if override.HasCompression {
		out.Compression, out.HasCompression = override.Compression, true
	}
	if override.HasFormat {
		out.Format, out.HasFormat = override.Format, true
	}
	if override.HasImageID {
		out.ImageID, out.HasImageID = override.ImageID, true
	}
	if override.HasImageNumber {
		out.ImageNumber, out.HasImageNumber = override.ImageNumber, true
	}
	if override.HasMore {
		out.More, out.HasMore = override.More, true
	}
	if override.HasQuiet {
		out.Quiet, out.HasQuiet = override.Quiet, true
	}
	if override.HasWidth {
		out.Width, out.HasWidth = override.Width, true
	}
	if override.HasHeight {
		out.Height, out.HasHeight = override.Height, true
	}
	if override.HasColumns {
		out.Columns, out.HasColumns = override.Columns, true
	}
	if override.HasRows {
		out.Rows, out.HasRows = override.Rows, true
	}
	if override.HasX {
		out.X, out.HasX = override.X, true
	}
	if override.HasY {
		out.Y, out.HasY = override.Y, true
	}
	if override.HasSourceWidth {
		out.SourceWidth, out.HasSourceWidth = override.SourceWidth, true
	}
	if override.HasSourceHeight {
		out.SourceHeight, out.HasSourceHeight = override.SourceHeight, true
	}
	if override.HasLayer {
		out.Layer, out.HasLayer = override.Layer, true
	}
	if override.HasPlacementID {
		out.PlacementID, out.HasPlacementID = override.PlacementID, true
	}
	if override.HasDelete {
		out.Delete, out.HasDelete = override.Delete, true
	}
	if override.HasTransmission {
		out.Transmission, out.HasTransmission = override.Transmission, true
	}
	if override.HasCursor {
		out.Cursor, out.HasCursor = override.Cursor, true
	}
	if override.HasSourceX {
		out.SourceX, out.HasSourceX = override.SourceX, true
	}
	if override.HasSourceY {
		out.SourceY, out.HasSourceY = override.SourceY, true
	}
	if override.HasCellOffsetX {
		out.CellOffsetX, out.HasCellOffsetX = override.CellOffsetX, true
	}
	if override.HasCellOffsetY {
		out.CellOffsetY, out.HasCellOffsetY = override.CellOffsetY, true
	}
	if override.HasParent {
		out.Parent, out.HasParent = override.Parent, true
	}
	return out
}
