package vt

import (
	"unicode/utf8"

	renderer "github.com/bnema/vev-vt/core"
	"github.com/bnema/vev-vt/protocol/kittygraphics"
)

// maxEscapeBufferLen must stay large enough for OSC 52 clipboard payloads
// forwarded by internal/usecase/copy.OSC52MaxPayloadBytes after base64
// expansion plus the OSC wrapper. pkg/vt cannot import internal/usecase/copy,
// so keep this value in sync if that payload cap changes.
const maxEscapeBufferLen = 128 * 1024

// Kitty graphics APCs have their own protocol limit. Keep this separate from
// the ordinary escape-string limit so a fragmented image does not get fed
// through the text path or truncated at the OSC clipboard bound.
const maxKittyEscapeBufferLen = int(kittygraphics.DefaultMaxAPCBytes) + 5

const (
	// ColorSchemeReportDark is the DEC 2031 dark-scheme report.
	ColorSchemeReportDark = "\x1b[?997;1n"
	// ColorSchemeReportLight is the DEC 2031 light-scheme report.
	ColorSchemeReportLight = "\x1b[?997;2n"
)

type Screen struct {
	Frame renderer.Frame
	Row   int
	Col   int
	Style renderer.Style
	// OnLineEvicted is called just before a full-width upward scroll recycles
	// and blanks rows. The callback receives a stable copy of each evicted row.
	OnLineEvicted func([]renderer.Cell)
	// OnResponse is called synchronously from Write with reply bytes that the
	// emulator must send back to the child process (DA, DSR, and ANSI/DEC mode
	// query reports). The host wires it to the PTY input. Nil disables responses.
	OnResponse func([]byte)
	// OnBell is called synchronously from Write for each lone BEL (0x07)
	// outside escape sequences. BELs that terminate an OSC never fire it.
	// Nil disables bell reporting.
	OnBell func()
	// OnNotify is called synchronously from Write for explicit terminal
	// notifications: OSC 9 (body only) and OSC 777 "notify" (title;body).
	// Other non-clipboard OSC payloads remain discarded. Nil disables it.
	OnNotify func(title, body string)
	// OnProgress is called synchronously from Write for OSC 9;4 progress
	// transitions that request attention: active progress cleared or first entry
	// into error state. Nil disables progress reporting.
	OnProgress func(errored bool)
	// OnClipboard is called synchronously from Write for a complete OSC 52
	// clipboard set request from the child. The OSC 52 selection field is
	// accepted but ignored; the callback receives only the raw base64 payload.
	// Clipboard queries (data == "?") and malformed payloads are ignored and
	// never invoke it. Nil disables it.
	OnClipboard func(b64 string)

	history *History
	buffer  *buffer

	nextRowID RowID
	geometry  Geometry

	defaultFG          renderer.RGB
	defaultBG          renderer.RGB
	defaultColorsKnown bool
	terminalTitle      string

	damage                 []renderer.Damage
	damageGeneration       uint64
	damageSaturated        bool
	damageFullRedrawSticky bool
	escapeBuf              []byte
	kittyDiscard           bool
	kittyDiscardEscaped    bool
	kittyPendingDisplay    *kittygraphics.Controls
	csiScratch             []int
	sgrScratch             []int

	scrollTop        int
	scrollBottom     int
	savedCursor      cursorState
	alternate        *screenState
	graphics         *screenGraphicsState
	syncUpdateActive bool
	progressState    int
	cursorVisible    bool
	cursorStyle      int
	cursorStyleSet   bool
	mouseMode        int
	mouseSGR         bool
	bracketedPaste   bool
	originMode       bool
	insertMode       bool
	colorSchemeMode  bool
	colorSchemeLight bool
	colorSchemeSet   bool
}

func NewScreen(width, height int) *Screen {
	s := &Screen{
		Style:            renderer.DefaultStyle(),
		damage:           []renderer.Damage{renderer.FullRedraw()},
		damageGeneration: 1,
		cursorVisible:    true,
		geometry:         Geometry{Cols: width, Rows: height},
	}
	s.buffer = s.newBuffer(width, height)
	s.Frame = s.buffer.frame
	s.resetScrollRegion()
	return s
}

// NewScreenWithHistory creates a screen that records rows evicted from its
// primary screen into bounded immutable terminal history.
func NewScreenWithHistory(width, height int, config HistoryConfig) *Screen {
	s := NewScreen(width, height)
	s.history = NewHistory(config)
	return s
}

// History returns this screen's terminal history, or nil when history was not
// configured with NewScreenWithHistory.
func (s *Screen) History() *History { return s.history }

// LineBounds returns an owned copy of the live grid's per-row logical extents,
// indexed like Frame rows. It returns nil when the screen has no buffer.
func (s *Screen) LineBounds() []LineBound {
	if s == nil || s.buffer == nil {
		return nil
	}
	return append([]LineBound(nil), s.buffer.boundaries...)
}

func (s *Screen) Write(data []byte) {
	if len(s.escapeBuf) > 0 && isKittyEscapeContinuation(s.escapeBuf, data) {
		prefix := s.escapeBuf
		s.escapeBuf = nil
		data = s.continueKittyEscape(prefix, data)
		if len(data) == 0 {
			return
		}
	}
	if len(s.escapeBuf) > 0 {
		combined := make([]byte, 0, len(s.escapeBuf)+len(data))
		combined = append(combined, s.escapeBuf...)
		combined = append(combined, data...)
		data = combined
		s.escapeBuf = nil
	}
	if s.kittyDiscard {
		consumed := s.consumeKittyDiscard(data)
		data = data[consumed:]
		if s.kittyDiscard {
			return
		}
	}

	for len(data) > 0 {
		if data[0] == 0x1b {
			consumed, partial := s.consumeEscape(data)
			if consumed > 0 {
				data = data[consumed:]
				continue
			}
			if partial {
				limit := maxEscapeBufferLen
				if isKittyEscapePrefix(data) {
					limit = maxKittyEscapeBufferLen
				}
				if len(data) <= limit {
					s.escapeBuf = append(s.escapeBuf[:0], data...)
				} else if isKittyEscapePrefix(data) {
					s.abortKittyPendingDisplay()
					s.kittyDiscard = true
					s.kittyDiscardEscaped = false
				}
				return
			}
		}
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			data = data[1:]
			continue
		}
		s.putRune(r)
		data = data[size:]
	}
}

func isKittyEscapePrefix(data []byte) bool {
	if len(data) == 0 || data[0] != 0x1b {
		return false
	}
	if len(data) > 1 && data[1] != '_' {
		return false
	}
	return len(data) < 3 || data[2] == 'G'
}

func isKittyEscapeContinuation(prefix, data []byte) bool {
	if !isKittyEscapePrefix(prefix) {
		return false
	}
	if len(data) == 0 {
		return true
	}
	if len(prefix) == 1 {
		return data[0] == '_'
	}
	if len(prefix) == 2 {
		return data[0] == 'G'
	}
	return true
}

func (s *Screen) continueKittyEscape(prefix, data []byte) []byte {
	if len(prefix) != 0 && prefix[len(prefix)-1] == 0x1b && len(data) != 0 && data[0] == '\\' {
		if len(prefix)+1 <= maxKittyEscapeBufferLen {
			apc := append(prefix, data[:1]...)
			s.dispatchKittyGraphics(apc)
		} else {
			s.abortKittyPendingDisplay()
		}
		return data[1:]
	}
	for i := 0; i < len(data); i++ {
		if data[i] == 0x9c {
			end := i + 1
			if len(prefix)+end <= maxKittyEscapeBufferLen {
				apc := append(prefix, data[:end]...)
				s.dispatchKittyGraphics(apc)
			} else {
				s.abortKittyPendingDisplay()
			}
			return data[end:]
		}
		if data[i] == 0x1b && i+1 < len(data) && data[i+1] == '\\' {
			end := i + 2
			if len(prefix)+end <= maxKittyEscapeBufferLen {
				apc := append(prefix, data[:end]...)
				s.dispatchKittyGraphics(apc)
			} else {
				s.abortKittyPendingDisplay()
			}
			return data[end:]
		}
	}
	remaining := maxKittyEscapeBufferLen - len(prefix)
	if len(data) <= remaining {
		returnData := append(prefix, data...)
		s.escapeBuf = returnData
		return nil
	}
	s.abortKittyPendingDisplay()
	s.kittyDiscard = true
	escaped := len(prefix) != 0 && prefix[len(prefix)-1] == 0x1b
	if remaining > 0 {
		escaped = data[remaining-1] == 0x1b
	}
	s.kittyDiscardEscaped = escaped
	tail := data[remaining:]
	return tail[s.consumeKittyDiscard(tail):]
}

func (s *Screen) consumeKittyDiscard(data []byte) int {
	for i, b := range data {
		if s.kittyDiscardEscaped {
			s.kittyDiscardEscaped = false
			if b == '\\' {
				s.kittyDiscard = false
				return i + 1
			}
		}
		if b == 0x9c {
			s.kittyDiscard = false
			return i + 1
		}
		if b == 0x1b {
			if i+1 < len(data) && data[i+1] == '\\' {
				s.kittyDiscard = false
				return i + 2
			}
			s.kittyDiscardEscaped = true
		}
	}
	return len(data)
}

func (s *Screen) consumeEscape(data []byte) (consumed int, partial bool) {
	if len(data) < 2 {
		return 0, true
	}
	switch data[1] {
	case ']':
		return s.consumeOSC(data)
	case 'P':
		return consumeSTString(data)
	case '_':
		if len(data) >= 3 && data[2] == 'G' {
			return s.consumeKittyGraphics(data)
		}
		return consumeSTString(data)
	case '^', 'X':
		return consumeSTString(data)
	case '[':
		return s.consumeCSI(data)
	case '=':
		return 2, false
	case '>':
		return 2, false
	case '7':
		s.saveCursor()
		return 2, false
	case '8':
		s.restoreCursor()
		return 2, false
	case 'D':
		s.buffer.hard(s.Row)
		s.index()
		return 2, false
	case 'E':
		s.nextLine()
		return 2, false
	case 'M':
		s.reverseIndex()
		return 2, false
	case 'c':
		s.reset()
		return 2, false
	case 'H':
		return 2, false
	case '(', ')', '*', '+', '-', '.', '/':
		if len(data) < 3 {
			return 0, true
		}
		return 3, false
	default:
		if data[1] >= 0x30 && data[1] <= 0x7e {
			return 2, false
		}
		return 0, false
	}
}

func consumeSTString(data []byte) (consumed int, partial bool) {
	for i := 2; i < len(data); i++ {
		if data[i] == 0x1b && i+1 < len(data) && data[i+1] == '\\' {
			return i + 2, false
		}
	}
	return 0, true
}

// consumeKittyGraphics frames one exact ESC _ G APC and dispatches only its
// complete bytes to the protocol adapter. The ordinary parser never sees the
// APC body as text, and the adapter is allocated only on the first complete
// graphics command.
func (s *Screen) consumeKittyGraphics(data []byte) (consumed int, partial bool) {
	for i := 3; i < len(data); i++ {
		switch data[i] {
		case 0x9c: // C1 string terminator.
			end := i + 1
			if end > maxKittyEscapeBufferLen {
				s.abortKittyPendingDisplay()
				return end, false
			}
			s.dispatchKittyGraphics(data[:end])
			return end, false
		case 0x1b:
			if i+1 < len(data) && data[i+1] == '\\' {
				end := i + 2
				if end > maxKittyEscapeBufferLen {
					s.abortKittyPendingDisplay()
					return end, false
				}
				s.dispatchKittyGraphics(data[:end])
				return end, false
			}
		}
	}
	return 0, true
}

func (s *Screen) dispatchKittyGraphics(apc []byte) {
	command, err := kittygraphics.ParseAPC(apc)
	if err != nil {
		s.abortKittyPendingDisplay()
		return
	}
	if s.graphics == nil {
		s.graphics = newScreenGraphicsState()
	}
	controls := command.Controls
	pending := s.kittyPendingDisplay
	continuation := pending != nil
	action := controls.Action
	if !controls.HasAction {
		action = kittygraphics.ActionTransmit
		if pending != nil {
			action = pending.Action
		}
	}
	display := action == kittygraphics.ActionTransmitDisplay || action == kittygraphics.ActionPut
	movementControls := controls
	if pending != nil {
		movementControls = *pending
	}
	if display && !continuation {
		if !(controls.HasMore && controls.More == 1) {
			if !controls.HasX {
				controls.X, controls.HasX = s.kittyCursorX(), true
			}
			if !controls.HasY {
				controls.Y, controls.HasY = s.kittyCursorY(), true
			}
			command.Controls = controls
		}
		movementControls = controls
	}
	if display && continuation && !(controls.HasMore && controls.More == 1) {
		s.graphics.kitty.SetPendingPlacement(s.kittyCursorX(), s.kittyCursorY())
	}
	if display && controls.HasMore && controls.More == 1 {
		copy := controls
		s.kittyPendingDisplay = &copy
	}
	result, processErr := s.graphics.kitty.Process(command)
	if processErr != nil {
		s.abortKittyPendingDisplay()
	}
	for _, response := range result.Responses {
		s.respond(response)
	}
	if len(result.Mutations) != 0 {
		if display {
			s.applyKittyCursorMovement(movementControls)
			s.kittyPendingDisplay = nil
		}
		// Graphics are rendered independently from Frame. A graphics mutation
		// still needs to wake consumers which only redraw on screen damage.
		s.fullRedraw()
	}
}

func (s *Screen) kittyCursorX() uint64 {
	if cellWidth, _, ok := s.kittyCellPixels(); ok {
		return uint64(s.Col * cellWidth)
	}
	return uint64(s.Col)
}

func (s *Screen) kittyCursorY() uint64 {
	_, cellHeight, ok := s.kittyCellPixels()
	if ok {
		return uint64(s.Row * cellHeight)
	}
	return uint64(s.Row)
}

func (s *Screen) kittyCellPixels() (width, height int, ok bool) {
	if !s.geometry.PixelsKnown() || s.geometry.Cols <= 0 || s.geometry.Rows <= 0 {
		return 0, 0, false
	}
	width = s.geometry.PixelWidth / s.geometry.Cols
	height = s.geometry.PixelHeight / s.geometry.Rows
	if width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

func (s *Screen) applyKittyCursorMovement(c kittygraphics.Controls) {
	if c.HasCursor && c.Cursor == 1 {
		return
	}
	columns, rows := 1, 1
	if c.HasColumns {
		columns = kittyCursorAdvance(c.Columns)
	}
	if c.HasRows {
		rows = kittyCursorAdvance(c.Rows)
	}
	s.Col = advanceKittyCursor(s.Col, columns, 0, s.Frame.Width-1)
	s.Row = advanceKittyCursor(s.Row, rows, s.cursorMinRow(), s.cursorMaxRow())
}

func advanceKittyCursor(current, amount, minimum, maximum int) int {
	if maximum < minimum {
		return minimum
	}
	if current < minimum {
		current = minimum
	}
	if current > maximum {
		current = maximum
	}
	if amount > maximum-current {
		return maximum
	}
	return current + amount
}

func kittyCursorAdvance(value uint64) int {
	if value > uint64(^uint(0)>>1) {
		return int(^uint(0) >> 1)
	}
	return int(value)
}
