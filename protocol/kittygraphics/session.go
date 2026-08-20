package kittygraphics

import (
	"bytes"
	"fmt"
	"image/png"
	"math"
	"strconv"

	"github.com/bnema/vev-vt/graphics"
)

type upload struct {
	controls Controls
	imageID  uint64
	payload  []byte
	chunks   uint64
}

// MutationKind describes a scene mutation performed by a session.
type MutationKind uint8

const (
	MutationUpload MutationKind = iota + 1
	MutationPlacement
	MutationDeleteImage
	MutationDeletePlacement
	MutationClear
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
	result := Result{}
	for _, event := range s.parser.Finish() {
		result.Events = append(result.Events, event)
	}
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

func (s *Session) apply(command Command) ([][]byte, *Mutation, error) {
	c := command.Controls
	action := c.Action
	if !c.HasAction {
		action = ActionTransmit
	}
	if c.HasCompression && c.Compression != 0 {
		return s.failure(c, controlsImageID(c), ErrUnsupported)
	}
	if c.HasTransmission && c.Transmission != TransmissionDirect {
		return s.failure(c, controlsImageID(c), ErrUnsupported)
	}
	if s.upload != nil && action != ActionTransmit && action != ActionTransmitDisplay {
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
		if len(command.Payload) != 0 {
			return s.failure(c, controlsImageID(c), ErrInvalidCommand)
		}
		return s.query(c)
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
	imageID := controlsImageID(c)
	if s.upload != nil {
		if !continuationControlsAllowed(c, s.upload.controls) {
			return s.failure(c, controlsImageID(c), ErrInterleavedUpload)
		}
		hasImageSelector := c.HasImageID || c.HasImageNumber
		if hasImageSelector && imageID != 0 && imageID != s.upload.imageID {
			return s.failure(c, imageID, ErrInterleavedUpload)
		}
		if hasImageSelector && imageID == 0 {
			return s.failure(c, s.upload.imageID, ErrInterleavedUpload)
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
		return s.commitUpload(complete, display)
	}

	if imageID == 0 {
		imageID = s.nextImageID()
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
		}
		return nil, nil, nil
	}
	return s.commitUpload(upload{controls: c, imageID: imageID, payload: append([]byte(nil), command.Payload...), chunks: 1}, display)
}

func (s *Session) commitUpload(value upload, display bool) ([][]byte, *Mutation, error) {
	if uint64(len(value.payload)) > s.limits.MaxPayloadBytes || uint64(len(value.payload)) > s.limits.MaxUploadBytes {
		return s.failure(value.controls, value.imageID, ErrPayloadTooLarge)
	}
	decoded, err := DecodeBase64(value.payload)
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
	if old, ok := s.images[value.imageID]; ok {
		// Remove adapter references while the old scene snapshot still
		// contains its placements, then remove the scene asset atomically.
		s.removeMappingsForAsset(old)
		if err := s.scene.RemoveAssetCascade(old); err != nil {
			return s.failure(value.controls, value.imageID, err)
		}
	}
	if uint64(len(s.images)) >= s.limits.MaxImages {
		return s.failure(value.controls, value.imageID, graphics.ErrTooManyAssets)
	}
	assetID, err := s.scene.AddAsset(graphics.AssetBlob{
		Encoded: decoded,
		Width:   width,
		Height:  height,
	})
	if err != nil {
		return s.failure(value.controls, value.imageID, err)
	}
	s.images[value.imageID] = assetID
	childID := s.nextChild
	s.nextChild++
	s.children[childID] = Child{ID: childID, ImageID: value.imageID, AssetID: assetID}
	mutation := &Mutation{Kind: MutationUpload, ImageID: value.imageID, ChildID: childID, AssetID: assetID}
	if display {
		responses, placementMutation, placeErr := s.place(value.controls, value.imageID, assetID)
		if placementMutation != nil {
			mutation.Kind = MutationPlacement
			mutation.PlacementID = placementMutation.PlacementID
			mutation.ScenePlacementID = placementMutation.ScenePlacementID
		}
		if placeErr != nil {
			// T (transmit and display) is one protocol operation. Do not
			// leave an uploaded asset behind when its placement is rejected.
			_ = s.scene.RemoveAssetCascade(assetID)
			delete(s.images, value.imageID)
			delete(s.children, childID)
			return responses, nil, placeErr
		}
		return s.success(value.controls, value.imageID, responses...), mutation, nil
	}
	return s.success(value.controls, value.imageID), mutation, nil
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
		if uint64(config.Width) > math.MaxInt64/uint64(config.Height) || uint64(config.Width)*uint64(config.Height) > limits.MaxDecodedPixels {
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
	if c.Width > limits.MaxDimension || c.Height > limits.MaxDimension || c.Width > math.MaxInt64/c.Height || c.Width*c.Height > limits.MaxDecodedPixels {
		return 0, 0, ErrPayloadTooLarge
	}
	channels := uint64(3)
	if format == FormatRGBA {
		channels = 4
	}
	if c.Width > math.MaxUint64/(c.Height*channels) {
		return 0, 0, ErrPayloadTooLarge
	}
	expected := c.Width * c.Height * channels
	if uint64(len(data)) != expected {
		return 0, 0, fmt.Errorf("%w: raw payload has %d bytes, want %d", ErrInvalidCommand, len(data), expected)
	}
	return int64(c.Width), int64(c.Height), nil
}

func (s *Session) put(c Controls) ([][]byte, *Mutation, error) {
	imageID := controlsImageID(c)
	if imageID == 0 {
		return s.failure(c, imageID, ErrImageNotFound)
	}
	assetID, ok := s.images[imageID]
	if !ok {
		return s.failure(c, imageID, ErrImageNotFound)
	}
	responses, mutation, err := s.place(c, imageID, assetID)
	if err != nil {
		return responses, mutation, err
	}
	return s.success(c, imageID, responses...), mutation, nil
}

func (s *Session) place(c Controls, imageID uint64, assetID graphics.AssetID) ([][]byte, *Mutation, error) {
	if s.scene == nil {
		return nil, nil, ErrNoScene
	}
	asset, ok := s.scene.Snapshot().Asset(assetID)
	if !ok {
		return nil, nil, ErrImageNotFound
	}
	width, height := asset.Width(), asset.Height()
	source := graphics.PixelRect{Width: width, Height: height}
	var err error
	if source.X, err = checkedCoordinate(c.SourceX, c.HasSourceX); err != nil {
		return nil, nil, err
	}
	if source.Y, err = checkedCoordinate(c.SourceY, c.HasSourceY); err != nil {
		return nil, nil, err
	}
	if source.Width, err = checkedDimension(c.SourceWidth, c.HasSourceWidth, width); err != nil {
		return nil, nil, err
	}
	if source.Height, err = checkedDimension(c.SourceHeight, c.HasSourceHeight, height); err != nil {
		return nil, nil, err
	}
	x, err := checkedCoordinate(c.X, c.HasX)
	if err != nil {
		return nil, nil, err
	}
	y, err := checkedCoordinate(c.Y, c.HasY)
	if err != nil {
		return nil, nil, err
	}
	destination := graphics.PixelRect{X: x, Y: y, Width: source.Width, Height: source.Height}
	if !source.Valid() || !destination.Valid() {
		return nil, nil, graphics.ErrInvalidRect
	}
	var cells graphics.CellRect
	if c.HasColumns || c.HasRows {
		if !c.HasColumns || !c.HasRows || c.Columns == 0 || c.Rows == 0 || c.Columns > math.MaxInt64 || c.Rows > math.MaxInt64 {
			return nil, nil, fmt.Errorf("%w: c/r", ErrInvalidCommand)
		}
		cells = graphics.CellRect{X: signedCoordinateInt(c.CellOffsetX, c.HasCellOffsetX), Y: signedCoordinateInt(c.CellOffsetY, c.HasCellOffsetY), Width: int64(c.Columns), Height: int64(c.Rows)}
		if !cells.Valid() {
			return nil, nil, graphics.ErrInvalidRect
		}
	}
	spec := graphics.PlacementSpec{Asset: assetID, Source: source, Destination: destination, Cells: cells}
	if c.HasLayer {
		spec.Layer = c.Layer
	}
	placementID := c.PlacementID
	if placementID == 0 {
		placementID = s.nextPlacementID()
	}
	if existing, ok := s.placements[placementID]; ok {
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
	s.placements[placementID] = sceneID
	return nil, &Mutation{Kind: MutationPlacement, ImageID: imageID, AssetID: assetID, PlacementID: placementID, ScenePlacementID: sceneID}, nil
}

func (s *Session) query(c Controls) ([][]byte, *Mutation, error) {
	id := controlsImageID(c)
	if id != 0 {
		if _, ok := s.images[id]; !ok {
			return s.failure(c, id, ErrImageNotFound)
		}
	}
	return s.success(c, id), nil, nil
}

func (s *Session) delete(c Controls) ([][]byte, *Mutation, error) {
	target := c.Delete
	if !c.HasDelete {
		target = DeleteImage
	}
	id := controlsImageID(c)
	switch target {
	case DeleteImage, DeleteImageNumber:
		asset, ok := s.images[id]
		if !ok {
			return s.failure(c, id, ErrImageNotFound)
		}
		if s.scene == nil {
			return s.failure(c, id, ErrNoScene)
		}
		s.removeMappingsForAsset(asset)
		if err := s.scene.RemoveAssetCascade(asset); err != nil {
			return s.failure(c, id, err)
		}
		delete(s.images, id)
		return s.success(c, id), &Mutation{Kind: MutationDeleteImage, ImageID: id, AssetID: asset}, nil
	case DeletePlacement:
		placementID := c.PlacementID
		mapped, ok := s.placements[placementID]
		if !ok || s.scene == nil {
			return s.failure(c, id, ErrPlacementNotFound)
		}
		if err := s.scene.RemovePlacement(mapped); err != nil {
			return s.failure(c, id, err)
		}
		delete(s.placements, placementID)
		return s.success(c, id), &Mutation{Kind: MutationDeletePlacement, PlacementID: placementID, ScenePlacementID: mapped}, nil
	case DeleteAll, DeleteAllImages, DeleteAllPlacements:
		if s.scene == nil {
			return s.failure(c, id, ErrNoScene)
		}
		if target == DeleteAllPlacements {
			for placementID, mapped := range s.placements {
				if err := s.scene.RemovePlacement(mapped); err != nil {
					return s.failure(c, id, err)
				}
				delete(s.placements, placementID)
			}
		} else {
			if err := s.scene.Clear(); err != nil {
				return s.failure(c, id, err)
			}
			s.images = make(map[uint64]graphics.AssetID)
			s.placements = make(map[uint64]graphics.PlacementID)
			s.children = make(map[uint64]Child)
		}
		return s.success(c, id), &Mutation{Kind: MutationClear}, nil
	default:
		return s.failure(c, id, ErrUnsupported)
	}
}

func (s *Session) removeMappingsForAsset(asset graphics.AssetID) {
	for imageID, mapped := range s.images {
		if mapped == asset {
			delete(s.images, imageID)
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

func controlsImageID(c Controls) uint64 {
	if c.HasImageID {
		return c.ImageID
	}
	if c.HasImageNumber {
		return c.ImageNumber
	}
	return 0
}

func (s *Session) nextImageID() uint64 {
	for id := uint64(1); ; id++ {
		if _, ok := s.images[id]; !ok {
			return id
		}
	}
}

func (s *Session) nextPlacementID() uint64 {
	max := uint64(0)
	for id := range s.placements {
		if id > max {
			max = id
		}
	}
	return max + 1
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
	if c.Quiet != QuietSuccess && c.Quiet != QuietAll {
		response := MakeResponse(imageID, "OK")
		if uint64(len(response)) <= s.limits.MaxResponseBytes {
			responses = append(responses, response)
		}
	}
	return responses
}

func (s *Session) failure(c Controls, imageID uint64, err error) ([][]byte, *Mutation, error) {
	if err == nil {
		err = ErrInvalidCommand
	}
	if c.Quiet != QuietAll {
		code := errorCode(err)
		response := MakeResponse(imageID, code)
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
