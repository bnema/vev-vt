// Package browser provides the static DOM runtime and strictly validated,
// transport-neutral browser event values for github.com/bnema/vev-vt/html.
package browser

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"unicode/utf8"

	htmlrenderer "github.com/bnema/vev-vt/html"
)

// EventSchemaVersion identifies the runtime-to-Go event contract.
const EventSchemaVersion = htmlrenderer.UpdateSchemaVersion

var (
	// ErrEventLimit reports that an event exceeded a configured resource bound.
	ErrEventLimit = errors.New("html/browser: event resource limit exceeded")
	// ErrInvalidEventLimits reports incompatible configured event bounds.
	ErrInvalidEventLimits = errors.New("html/browser: invalid event limits")
)

const (
	maxJSONEscapedByteLength = 6
	eventEnvelopeOverhead    = 64
)

// EventLimits bounds untrusted browser event decoding. Zero fields select safe
// defaults.
type EventLimits struct {
	MaxEventBytes int
	MaxTextBytes  int
	MaxPasteBytes int
	MaxColumns    int
	MaxRows       int
}

// DefaultEventLimits returns the bounds used for zero-valued event limits.
func DefaultEventLimits() EventLimits {
	return EventLimits{
		MaxEventBytes: 8 << 20,
		MaxTextBytes:  64 << 10,
		MaxPasteBytes: 1 << 20,
		MaxColumns:    10_000,
		MaxRows:       10_000,
	}
}

func normalizeEventLimits(limits EventLimits) (EventLimits, error) {
	if limits.MaxEventBytes < 0 || limits.MaxTextBytes < 0 || limits.MaxPasteBytes < 0 || limits.MaxColumns < 0 || limits.MaxRows < 0 {
		return EventLimits{}, fmt.Errorf("html/browser: event limits must not be negative")
	}
	defaults := DefaultEventLimits()
	if limits.MaxEventBytes == 0 {
		limits.MaxEventBytes = defaults.MaxEventBytes
	}
	if limits.MaxTextBytes == 0 {
		limits.MaxTextBytes = defaults.MaxTextBytes
	}
	if limits.MaxPasteBytes == 0 {
		limits.MaxPasteBytes = defaults.MaxPasteBytes
	}
	if limits.MaxColumns == 0 {
		limits.MaxColumns = defaults.MaxColumns
	}
	if limits.MaxRows == 0 {
		limits.MaxRows = defaults.MaxRows
	}
	requiredEventBytes, representable := minimumEventBytes(max(limits.MaxTextBytes, limits.MaxPasteBytes))
	if !representable || limits.MaxEventBytes < requiredEventBytes {
		return EventLimits{}, fmt.Errorf("%w: MaxEventBytes %d cannot contain the largest payload limit", ErrInvalidEventLimits, limits.MaxEventBytes)
	}
	return limits, nil
}

func minimumEventBytes(payloadBytes int) (int, bool) {
	maxInt := int(^uint(0) >> 1)
	if payloadBytes > (maxInt-eventEnvelopeOverhead)/maxJSONEscapedByteLength {
		return 0, false
	}
	return payloadBytes*maxJSONEscapedByteLength + eventEnvelopeOverhead, true
}

// EventKind identifies one closed browser event payload.
type EventKind string

// Supported browser event kinds.
const (
	EventText    EventKind = "text"
	EventKey     EventKind = "key"
	EventPaste   EventKind = "paste"
	EventPointer EventKind = "pointer"
	EventWheel   EventKind = "wheel"
	EventResize  EventKind = "resize"
	EventFocus   EventKind = "focus"
)

// Modifiers is the normalized browser modifier-key state.
type Modifiers struct {
	Alt   bool `json:"alt"`
	Ctrl  bool `json:"ctrl"`
	Meta  bool `json:"meta"`
	Shift bool `json:"shift"`
}

// TextEvent carries composed text input.
type TextEvent struct{ Text string }

// PasteEvent carries bounded plain-text clipboard input. DecodeEvent preserves
// Text unchanged, including control bytes such as ESC and CR. Consumers that
// forward it to a PTY must apply their required framing or filtering policy.
type PasteEvent struct{ Text string }

// KeyEvent carries a non-text key or modified key press.
type KeyEvent struct {
	Key       string
	Code      string
	Modifiers Modifiers
	Repeat    bool
	Location  int
}

// PointerEvent carries a pointer action mapped to terminal coordinates.
type PointerEvent struct {
	Action    string
	Button    int
	Buttons   uint16
	Row       int
	Column    int
	X         float64
	Y         float64
	Modifiers Modifiers
}

// WheelEvent carries browser wheel deltas and terminal coordinates.
type WheelEvent struct {
	DeltaX    float64
	DeltaY    float64
	DeltaMode int
	Row       int
	Column    int
	Modifiers Modifiers
}

// ResizeEvent carries measured grid and CSS-pixel geometry.
type ResizeEvent struct {
	Columns          int
	Rows             int
	PixelWidth       float64
	PixelHeight      float64
	CellWidth        float64
	CellHeight       float64
	DevicePixelRatio float64
}

// FocusEvent reports input-proxy focus changes.
type FocusEvent struct{ Focused bool }

// Event is a closed tagged union. Exactly one payload matching Kind is set.
type Event struct {
	SchemaVersion uint16
	Kind          EventKind
	Text          *TextEvent
	Key           *KeyEvent
	Paste         *PasteEvent
	Pointer       *PointerEvent
	Wheel         *WheelEvent
	Resize        *ResizeEvent
	Focus         *FocusEvent
}

type envelope struct {
	SchemaVersion uint16    `json:"schemaVersion"`
	Kind          EventKind `json:"type"`
}

type textWire struct {
	SchemaVersion uint16    `json:"schemaVersion"`
	Kind          EventKind `json:"type"`
	Text          string    `json:"text"`
}

type keyWire struct {
	SchemaVersion uint16    `json:"schemaVersion"`
	Kind          EventKind `json:"type"`
	Key           string    `json:"key"`
	Code          string    `json:"code"`
	Alt           bool      `json:"alt"`
	Ctrl          bool      `json:"ctrl"`
	Meta          bool      `json:"meta"`
	Shift         bool      `json:"shift"`
	Repeat        bool      `json:"repeat"`
	Location      int       `json:"location"`
}

type pointerWire struct {
	SchemaVersion uint16    `json:"schemaVersion"`
	Kind          EventKind `json:"type"`
	Action        string    `json:"action"`
	Button        int       `json:"button"`
	Buttons       uint16    `json:"buttons"`
	Row           int       `json:"row"`
	Column        int       `json:"column"`
	X             float64   `json:"x"`
	Y             float64   `json:"y"`
	Alt           bool      `json:"alt"`
	Ctrl          bool      `json:"ctrl"`
	Meta          bool      `json:"meta"`
	Shift         bool      `json:"shift"`
}

type wheelWire struct {
	SchemaVersion uint16    `json:"schemaVersion"`
	Kind          EventKind `json:"type"`
	DeltaX        float64   `json:"deltaX"`
	DeltaY        float64   `json:"deltaY"`
	DeltaMode     int       `json:"deltaMode"`
	Row           int       `json:"row"`
	Column        int       `json:"column"`
	Alt           bool      `json:"alt"`
	Ctrl          bool      `json:"ctrl"`
	Meta          bool      `json:"meta"`
	Shift         bool      `json:"shift"`
}

type resizeWire struct {
	SchemaVersion    uint16    `json:"schemaVersion"`
	Kind             EventKind `json:"type"`
	Columns          int       `json:"columns"`
	Rows             int       `json:"rows"`
	PixelWidth       float64   `json:"pixelWidth"`
	PixelHeight      float64   `json:"pixelHeight"`
	CellWidth        float64   `json:"cellWidth"`
	CellHeight       float64   `json:"cellHeight"`
	DevicePixelRatio float64   `json:"devicePixelRatio"`
}

type focusWire struct {
	SchemaVersion uint16    `json:"schemaVersion"`
	Kind          EventKind `json:"type"`
	Focused       bool      `json:"focused"`
}

// DecodeEvent strictly decodes one complete untrusted browser event.
func DecodeEvent(data []byte, requested EventLimits) (Event, error) {
	limits, err := normalizeEventLimits(requested)
	if err != nil {
		return Event{}, err
	}
	if len(data) > limits.MaxEventBytes {
		return Event{}, fmt.Errorf("%w: event has %d bytes, limit is %d", ErrEventLimit, len(data), limits.MaxEventBytes)
	}
	if !utf8.Valid(data) {
		return Event{}, fmt.Errorf("html/browser: event is not valid UTF-8")
	}
	var header envelope
	if err := json.Unmarshal(data, &header); err != nil {
		return Event{}, fmt.Errorf("html/browser: decode event envelope: %w", err)
	}
	if header.SchemaVersion != EventSchemaVersion {
		return Event{}, fmt.Errorf("html/browser: unsupported event schema %d", header.SchemaVersion)
	}

	event := Event{SchemaVersion: header.SchemaVersion, Kind: header.Kind}
	switch header.Kind {
	case EventText, EventPaste:
		var wire textWire
		if err := decodeStrict(data, &wire); err != nil {
			return Event{}, err
		}
		if !utf8.ValidString(wire.Text) {
			return Event{}, fmt.Errorf("html/browser: event text is not valid UTF-8")
		}
		limit := limits.MaxTextBytes
		if header.Kind == EventPaste {
			limit = limits.MaxPasteBytes
		}
		if len(wire.Text) > limit {
			return Event{}, fmt.Errorf("%w: %s has %d bytes, limit is %d", ErrEventLimit, header.Kind, len(wire.Text), limit)
		}
		if header.Kind == EventText {
			event.Text = &TextEvent{Text: wire.Text}
		} else {
			event.Paste = &PasteEvent{Text: wire.Text}
		}
	case EventKey:
		var wire keyWire
		if err := decodeStrict(data, &wire); err != nil {
			return Event{}, err
		}
		if wire.Key == "" || wire.Code == "" || len(wire.Key) > 128 || len(wire.Code) > 128 || wire.Location < 0 || wire.Location > 3 {
			return Event{}, fmt.Errorf("html/browser: invalid key event")
		}
		event.Key = &KeyEvent{Key: wire.Key, Code: wire.Code, Modifiers: modifiers(wire.Alt, wire.Ctrl, wire.Meta, wire.Shift), Repeat: wire.Repeat, Location: wire.Location}
	case EventPointer:
		var wire pointerWire
		if err := decodeStrict(data, &wire); err != nil {
			return Event{}, err
		}
		if !oneOf(wire.Action, "down", "up", "move", "cancel") || wire.Button < -1 || wire.Button > 4 || wire.Buttons > 31 || !validCell(wire.Row, wire.Column, limits) || !finiteBounded(wire.X) || !finiteBounded(wire.Y) {
			return Event{}, fmt.Errorf("html/browser: invalid pointer event")
		}
		event.Pointer = &PointerEvent{Action: wire.Action, Button: wire.Button, Buttons: wire.Buttons, Row: wire.Row, Column: wire.Column, X: wire.X, Y: wire.Y, Modifiers: modifiers(wire.Alt, wire.Ctrl, wire.Meta, wire.Shift)}
	case EventWheel:
		var wire wheelWire
		if err := decodeStrict(data, &wire); err != nil {
			return Event{}, err
		}
		if wire.DeltaMode < 0 || wire.DeltaMode > 2 || !validCell(wire.Row, wire.Column, limits) || !finiteBounded(wire.DeltaX) || !finiteBounded(wire.DeltaY) {
			return Event{}, fmt.Errorf("html/browser: invalid wheel event")
		}
		event.Wheel = &WheelEvent{DeltaX: wire.DeltaX, DeltaY: wire.DeltaY, DeltaMode: wire.DeltaMode, Row: wire.Row, Column: wire.Column, Modifiers: modifiers(wire.Alt, wire.Ctrl, wire.Meta, wire.Shift)}
	case EventResize:
		var wire resizeWire
		if err := decodeStrict(data, &wire); err != nil {
			return Event{}, err
		}
		if wire.Columns <= 0 || wire.Columns > limits.MaxColumns || wire.Rows <= 0 || wire.Rows > limits.MaxRows || !positiveFinite(wire.PixelWidth) || !positiveFinite(wire.PixelHeight) || !positiveFinite(wire.CellWidth) || !positiveFinite(wire.CellHeight) || !positiveFinite(wire.DevicePixelRatio) {
			return Event{}, fmt.Errorf("html/browser: invalid resize event")
		}
		event.Resize = &ResizeEvent{Columns: wire.Columns, Rows: wire.Rows, PixelWidth: wire.PixelWidth, PixelHeight: wire.PixelHeight, CellWidth: wire.CellWidth, CellHeight: wire.CellHeight, DevicePixelRatio: wire.DevicePixelRatio}
	case EventFocus:
		var wire focusWire
		if err := decodeStrict(data, &wire); err != nil {
			return Event{}, err
		}
		event.Focus = &FocusEvent{Focused: wire.Focused}
	default:
		return Event{}, fmt.Errorf("html/browser: unknown event kind %q", header.Kind)
	}
	return event, nil
}

func decodeStrict(data []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("html/browser: decode event: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("html/browser: event must contain one JSON value")
	}
	return nil
}

func modifiers(alt, ctrl, meta, shift bool) Modifiers {
	return Modifiers{Alt: alt, Ctrl: ctrl, Meta: meta, Shift: shift}
}

func validCell(row, column int, limits EventLimits) bool {
	return row >= 0 && row < limits.MaxRows && column >= 0 && column < limits.MaxColumns
}

func finiteBounded(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && math.Abs(value) <= 10_000_000
}

func positiveFinite(value float64) bool { return value > 0 && finiteBounded(value) }

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
