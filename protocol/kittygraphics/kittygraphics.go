package kittygraphics

import (
	"errors"
	"math"
	"sort"
	"sync"

	"github.com/bnema/vev-vt/graphics"
)

var (
	ErrInvalidAPC        = errors.New("invalid kitty graphics APC")
	ErrAPCTooLarge       = errors.New("kitty graphics APC is too large")
	ErrAPCTruncated      = errors.New("truncated kitty graphics APC")
	ErrInvalidCommand    = errors.New("invalid kitty graphics command")
	ErrUnknownAction     = errors.New("unknown kitty graphics action")
	ErrUnknownControl    = errors.New("unknown kitty graphics control")
	ErrDuplicateControl  = errors.New("duplicate kitty graphics control")
	ErrInvalidInteger    = errors.New("invalid kitty graphics integer")
	ErrIntegerOverflow   = errors.New("kitty graphics integer overflow")
	ErrInvalidBase64     = errors.New("invalid kitty graphics base64")
	ErrInvalidPNG        = errors.New("invalid kitty graphics PNG")
	ErrPayloadTooLarge   = errors.New("kitty graphics payload is too large")
	ErrTooManyChunks     = errors.New("too many kitty graphics continuation chunks")
	ErrInterleavedUpload = errors.New("interleaved kitty graphics upload")
	ErrUnsupported       = errors.New("unsupported kitty graphics command")
	ErrImageNotFound     = errors.New("kitty graphics image not found")
	ErrPlacementNotFound = errors.New("kitty graphics placement not found")
	ErrNoScene           = errors.New("kitty graphics scene is nil")
)

const (
	// DefaultMaxAPCBytes bounds the complete framed APC, including its
	// controls and encoded payload.
	DefaultMaxAPCBytes = uint64(32 << 20)
	// DefaultMaxPayloadBytes bounds encoded bytes in one image, before base64
	// decoding. The graphics scene applies its own encoded-byte budget too.
	DefaultMaxPayloadBytes = uint64(24 << 20)
	DefaultMaxUploadBytes  = uint64(16 << 20)
	DefaultMaxChunks       = uint64(1024)
	DefaultMaxPixels       = uint64(128 << 20)
	DefaultMaxDecodedBytes = uint64(256 << 20)
	DefaultMaxDimension    = uint64(1 << 20)
)

// Limits bounds parser and adapter resource use. Zero values select bounded
// defaults. The limits are independent from the graphics scene limits: both
// layers enforce their budgets.
type Limits struct {
	MaxAPCBytes      uint64
	MaxPayloadBytes  uint64
	MaxUploadBytes   uint64
	MaxChunks        uint64
	MaxDecodedPixels uint64
	MaxDecodedBytes  uint64
	MaxDimension     uint64
	MaxResponseBytes uint64
	MaxImages        uint64
	MaxPlacements    uint64
}

// Config is an alias retained for callers that use constructor terminology.
type Config = Limits

func normalizeLimits(l Limits) Limits {
	if l.MaxAPCBytes == 0 {
		l.MaxAPCBytes = DefaultMaxAPCBytes
	}
	if l.MaxPayloadBytes == 0 {
		l.MaxPayloadBytes = DefaultMaxPayloadBytes
	}
	if l.MaxUploadBytes == 0 {
		l.MaxUploadBytes = DefaultMaxUploadBytes
	}
	if l.MaxChunks == 0 {
		l.MaxChunks = DefaultMaxChunks
	}
	if l.MaxDecodedPixels == 0 {
		l.MaxDecodedPixels = DefaultMaxPixels
	}
	if l.MaxDecodedBytes == 0 {
		l.MaxDecodedBytes = DefaultMaxDecodedBytes
	}
	if l.MaxDimension == 0 {
		l.MaxDimension = DefaultMaxDimension
	}
	if l.MaxResponseBytes == 0 {
		l.MaxResponseBytes = 4096
	}
	if l.MaxImages == 0 {
		l.MaxImages = 1024
	}
	if l.MaxPlacements == 0 {
		l.MaxPlacements = 65536
	}
	return l
}

// Action is the closed set of Kitty graphics actions understood by this
// adapter. The underlying byte values are the protocol values.
type Action byte

const (
	ActionTransmit        Action = 't'
	ActionTransmitDisplay Action = 'T'
	ActionPut             Action = 'p'
	ActionQuery           Action = 'q'
	ActionDelete          Action = 'd'
	ActionFrame           Action = 'f'
	ActionCompose         Action = 'a'

	// Descriptive aliases used by callers that prefer the protocol names.
	ActionTransmitAndDisplay = ActionTransmitDisplay
	ActionDisplay            = ActionPut
)

func (a Action) Valid() bool {
	switch a {
	case ActionTransmit, ActionTransmitDisplay, ActionPut, ActionQuery, ActionDelete, ActionFrame, ActionCompose:
		return true
	default:
		return false
	}
}

func (a Action) String() string {
	if !a.Valid() {
		return "unknown"
	}
	return string([]byte{byte(a)})
}

// Format is the closed set of Kitty image formats supported by this adapter.
type Format uint64

const (
	FormatRGB  Format = 24
	FormatRGBA Format = 32
	FormatPNG  Format = 100

	FormatRGB24  = FormatRGB
	FormatRGBA32 = FormatRGBA
)

func (f Format) Valid() bool { return f == FormatRGB || f == FormatRGBA || f == FormatPNG }

// Compression is the optional compression applied to raw image data.
type Compression byte

const CompressionZlib Compression = 'z'

// Quiet controls protocol response emission. Kitty uses q=1 for successful
// operations only and q=2 for all responses.
type Quiet uint8

const (
	QuietNever   Quiet = 0
	QuietSuccess Quiet = 1
	QuietAll     Quiet = 2

	QuietNone       = QuietNever
	QuietErrorsOnly = QuietSuccess
	QuietEverything = QuietAll
)

// DeleteTarget is the closed set of targets accepted by a delete action.
type DeleteTarget byte

const (
	DeleteImage       DeleteTarget = 'i'
	DeleteImageNumber DeleteTarget = 'I'
	DeleteCell        DeleteTarget = 'p'
	DeleteAll         DeleteTarget = 'a'
	DeleteAllImages   DeleteTarget = 'A'
	DeleteCellAll     DeleteTarget = 'P'
)

// Transmission is the direct transmission mode. File, temporary-file and
// shared-memory transmissions are intentionally not accepted by this adapter.
type Transmission byte

const TransmissionDirect Transmission = 'd'

// ControlKey identifies a recognized Kitty control field.
type ControlKey byte

const (
	ControlAction       ControlKey = 'a'
	ControlCompression  ControlKey = 'o'
	ControlFormat       ControlKey = 'f'
	ControlImageID      ControlKey = 'i'
	ControlImageNumber  ControlKey = 'I'
	ControlMore         ControlKey = 'm'
	ControlQuiet        ControlKey = 'q'
	ControlWidth        ControlKey = 's'
	ControlHeight       ControlKey = 'v'
	ControlColumns      ControlKey = 'c'
	ControlRows         ControlKey = 'r'
	ControlX            ControlKey = 'x'
	ControlY            ControlKey = 'y'
	ControlSourceWidth  ControlKey = 'w'
	ControlSourceHeight ControlKey = 'h'
	ControlLayer        ControlKey = 'z'
	ControlPlacementID  ControlKey = 'p'
	ControlDelete       ControlKey = 'd'
	ControlTransmission ControlKey = 't'
	ControlCursor       ControlKey = 'C'
	ControlSourceX      ControlKey = 'X'
	ControlSourceY      ControlKey = 'Y'
	ControlCellOffsetX  ControlKey = 'H'
	ControlCellOffsetY  ControlKey = 'V'
	ControlParent       ControlKey = 'P'
)

// Controls is the typed, closed representation of a Kitty control header.
// Has* fields preserve the distinction between an omitted field and zero.
type Controls struct {
	Action          Action
	HasAction       bool
	Compression     Compression
	HasCompression  bool
	Format          Format
	HasFormat       bool
	ImageID         uint64
	HasImageID      bool
	ImageNumber     uint64
	HasImageNumber  bool
	More            uint64
	HasMore         bool
	Quiet           Quiet
	HasQuiet        bool
	Width           uint64
	HasWidth        bool
	Height          uint64
	HasHeight       bool
	Columns         uint64
	HasColumns      bool
	Rows            uint64
	HasRows         bool
	X               uint64
	HasX            bool
	Y               uint64
	HasY            bool
	SourceWidth     uint64
	HasSourceWidth  bool
	SourceHeight    uint64
	HasSourceHeight bool
	Layer           int64
	HasLayer        bool
	PlacementID     uint64
	HasPlacementID  bool
	Delete          DeleteTarget
	HasDelete       bool
	Transmission    Transmission
	HasTransmission bool
	Cursor          uint64
	HasCursor       bool
	SourceX         uint64
	HasSourceX      bool
	SourceY         uint64
	HasSourceY      bool
	CellOffsetX     int64
	HasCellOffsetX  bool
	CellOffsetY     int64
	HasCellOffsetY  bool
	Parent          uint64
	HasParent       bool
}

// Command is one complete APC graphics command. Payload is the encoded
// command payload and is copied from the parser input.
type Command struct {
	Controls Controls
	Payload  []byte
}

// EventKind identifies parser output. Text events contain bytes which are not
// Kitty graphics APCs; command events contain complete commands.
type EventKind uint8

const (
	EventText EventKind = iota + 1
	EventCommand
	EventError
)

// Event is emitted by Parser. Text and command payloads are owned by the
// event and remain valid after the next Feed call.
type Event struct {
	Kind    EventKind
	Text    []byte
	Command Command
	Err     error
}

// ParserConfig is a descriptive alias for Limits.
type ParserConfig = Limits

// Parser incrementally recognizes ESC _ G APCs. It never interprets ordinary
// text or other escape sequences and can be fed arbitrary byte-sized chunks.
type Parser struct {
	limits    Limits
	candidate []byte
	apc       []byte
	inAPC     bool
	escaped   bool
	oversize  bool
}

// NewParser creates an incremental bounded parser.
func NewParser(config ...Limits) *Parser {
	var l Limits
	if len(config) != 0 {
		l = config[0]
	}
	return &Parser{limits: normalizeLimits(l)}
}

// Limits reports the parser limits.
func (p *Parser) Limits() Limits {
	if p == nil {
		return Limits{}
	}
	return p.limits
}

// MaxInt64 is exported for callers constructing checked protocol geometry.
const MaxInt64 = int64(math.MaxInt64)

// MaxKittyID is the largest protocol image or placement identifier.
const MaxKittyID = uint64(^uint32(0))

// Session owns protocol-to-scene mappings. It is safe for one owner to call
// Feed and query methods serially; the mutex also makes read-only mapping
// access safe while a caller publishes a snapshot.
type Session struct {
	mu            sync.Mutex
	scene         *graphics.Scene
	limits        Limits
	parser        *Parser
	images        map[uint64]graphics.AssetID
	imageNumbers  map[uint64]uint64
	placements    map[placementKey]graphics.PlacementID
	children      map[uint64]Child
	nextChild     uint64
	nextPlacement uint64
	upload        *upload
	origin        placementOrigin
}

// Adapter is an alias for the stateful Kitty graphics session.
type Adapter = Session

// Child describes a session-local image child and its graphics asset. Child
// IDs are never sent to the terminal; they make the protocol mapping explicit
// and stable for consumers.
type Child struct {
	ID      uint64
	ImageID uint64
	AssetID graphics.AssetID
}

// NewSession constructs an adapter over scene. A nil scene is allowed so the
// parser can still be used for validation, but commands which mutate a scene
// return ErrNoScene.
func NewSession(scene *graphics.Scene, config ...Limits) *Session {
	var l Limits
	if len(config) != 0 {
		l = config[0]
	}
	l = normalizeLimits(l)
	return &Session{
		scene:         scene,
		limits:        l,
		parser:        NewParser(l),
		images:        make(map[uint64]graphics.AssetID),
		imageNumbers:  make(map[uint64]uint64),
		placements:    make(map[placementKey]graphics.PlacementID),
		children:      make(map[uint64]Child),
		nextChild:     1,
		nextPlacement: 1,
	}
}

// NewAdapter is an adapter-named constructor alias.
func NewAdapter(scene *graphics.Scene, config ...Limits) *Session {
	return NewSession(scene, config...)
}

// New is a concise constructor alias for NewSession.
func New(scene *graphics.Scene, config ...Limits) *Session { return NewSession(scene, config...) }

// Image returns the graphics asset mapped from a Kitty image ID.
func (s *Session) Image(imageID uint64) (graphics.AssetID, bool) {
	if s == nil {
		return graphics.AssetID{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.images[imageID]
	return id, ok
}

// Asset is an alias for Image.
func (s *Session) Asset(imageID uint64) (graphics.AssetID, bool) { return s.Image(imageID) }

// Placement returns the graphics placement mapped from one Kitty image and
// placement-ID pair. Kitty placement IDs are scoped to their image ID.
func (s *Session) Placement(imageID, placementID uint64) (graphics.PlacementID, bool) {
	if s == nil {
		return graphics.PlacementID{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.placements[placementKey{imageID: imageID, placementID: placementID}]
	return id, ok
}

// PlacementID is an alias for Placement.
func (s *Session) PlacementID(imageID, placementID uint64) (graphics.PlacementID, bool) {
	return s.Placement(imageID, placementID)
}

// Child returns the oldest session child associated with an image ID.
func (s *Session) Child(imageID uint64) (Child, bool) {
	if s == nil {
		return Child{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var selected Child
	found := false
	for _, child := range s.children {
		if child.ImageID == imageID && (!found || child.ID < selected.ID) {
			selected, found = child, true
		}
	}
	return selected, found
}

// ChildByID returns a session child by its adapter-local ID.
func (s *Session) ChildByID(childID uint64) (Child, bool) {
	if s == nil {
		return Child{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	child, ok := s.children[childID]
	return child, ok
}

// ChildID returns the adapter-local child ID associated with an image ID.
func (s *Session) ChildID(imageID uint64) (uint64, bool) {
	child, ok := s.Child(imageID)
	if !ok {
		return 0, false
	}
	return child.ID, true
}

// Children returns a stable copy of all current session children in child-ID
// order.
func (s *Session) Children() []Child {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	children := make([]Child, 0, len(s.children))
	for _, child := range s.children {
		children = append(children, child)
	}
	sort.Slice(children, func(i, j int) bool { return children[i].ID < children[j].ID })
	return children
}
