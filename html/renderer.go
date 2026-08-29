package html

import (
	"encoding/json"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/bnema/vev-vt/core"
)

type Renderer struct {
	limits      Limits
	generation  uint64
	pending     *transaction
	committed   core.Frame
	cursor      Cursor
	initialized bool
}

type transactionState uint8

const (
	transactionPending transactionState = iota
	transactionCommitted
	transactionAborted
)

type transaction struct {
	renderer   *Renderer
	generation uint64
	state      transactionState
	update     Update
	json       []byte
	frame      core.Frame
	cursor     Cursor
}

// PreparedDraw owns one immutable update and its speculative renderer state.
type PreparedDraw struct {
	tx *transaction
}

func New(options Options) (*Renderer, error) {
	limits, err := normalizeLimits(options.Limits)
	if err != nil {
		return nil, err
	}
	return &Renderer{limits: limits, generation: 1}, nil
}

// Prepare validates and encodes a speculative update. The renderer advances
// only after Commit. Abort preserves the previous committed shadow.
func (r *Renderer) Prepare(frame core.Frame, damage []core.Damage, reset bool, cursor Cursor) (*PreparedDraw, error) {
	if r == nil {
		return nil, fmt.Errorf("html: nil renderer")
	}
	if r.pending != nil {
		return nil, ErrPendingDraw
	}
	if err := r.validateFrame(frame); err != nil {
		return nil, err
	}
	if err := validateCursor(cursor, frame.Width, frame.Height); err != nil {
		return nil, err
	}

	snapshot := reset || !r.initialized || r.committed.Width != frame.Width || r.committed.Height != frame.Height || damageRequiresSnapshot(damage, frame.Width, frame.Height)
	candidate := frame.Clone()
	update, err := r.buildUpdate(candidate, snapshot, cursor)
	if err != nil {
		return nil, err
	}
	if estimate, ok := estimatedUpdateBytes(update, r.limits.MaxGeneratedBytes); !ok {
		return nil, fmt.Errorf("%w: estimated update is at least %d bytes, limit is %d", ErrLimitExceeded, estimate, r.limits.MaxGeneratedBytes)
	}
	encoded, err := json.Marshal(update)
	if err != nil {
		return nil, fmt.Errorf("html: encode update: %w", err)
	}
	if len(encoded) > r.limits.MaxGeneratedBytes {
		return nil, fmt.Errorf("%w: generated update is %d bytes, limit is %d", ErrLimitExceeded, len(encoded), r.limits.MaxGeneratedBytes)
	}

	tx := &transaction{
		renderer:   r,
		generation: r.generation,
		state:      transactionPending,
		update:     update,
		json:       encoded,
		frame:      candidate,
		cursor:     cursor,
	}
	r.pending = tx
	return &PreparedDraw{tx: tx}, nil
}

func (r *Renderer) validateFrame(frame core.Frame) error {
	if frame.Width <= 0 || frame.Height <= 0 {
		return fmt.Errorf("html: invalid frame size %dx%d", frame.Width, frame.Height)
	}
	if frame.Width > math.MaxInt/frame.Height {
		return fmt.Errorf("%w: frame cell count overflows int", ErrLimitExceeded)
	}
	cells := frame.Width * frame.Height
	if cells > r.limits.MaxCells {
		return fmt.Errorf("%w: frame has %d cells, limit is %d", ErrLimitExceeded, cells, r.limits.MaxCells)
	}
	if frame.Height > r.limits.MaxRowsPerUpdate {
		return fmt.Errorf("%w: frame has %d rows, limit is %d", ErrLimitExceeded, frame.Height, r.limits.MaxRowsPerUpdate)
	}
	if err := frame.Validate(); err != nil {
		return fmt.Errorf("html: validate frame: %w", err)
	}
	for y := range frame.Height {
		row := frame.Row(y)
		for x := 0; x < frame.Width; x++ {
			cell := row[x]
			if err := validateCoreStyle(cell.Style); err != nil {
				return fmt.Errorf("html: cell (%d,%d): %w", x, y, err)
			}
			if cell.Continuation {
				if x == 0 || row[x-1].Continuation || core.RuneWidth(row[x-1].Rune) != 2 {
					return fmt.Errorf("html: cell (%d,%d): orphan wide continuation", x, y)
				}
				continue
			}
			if cell.Rune == 0 {
				continue
			}
			if !utf8.ValidRune(cell.Rune) {
				return fmt.Errorf("html: cell (%d,%d): invalid Unicode scalar", x, y)
			}
			width := core.RuneWidth(cell.Rune)
			switch width {
			case 1:
			case 2:
				if x+1 >= frame.Width || !row[x+1].Continuation {
					return fmt.Errorf("html: cell (%d,%d): wide rune lacks continuation", x, y)
				}
			case 0:
				return fmt.Errorf("html: cell (%d,%d): unsupported zero-width rune", x, y)
			default:
				return fmt.Errorf("html: cell (%d,%d): unsupported rune width %d", x, y, width)
			}
		}
	}
	return nil
}

func validateCursor(cursor Cursor, width, height int) error {
	if cursor.Row < 0 || cursor.Row >= height || cursor.Column < 0 || cursor.Column >= width {
		return fmt.Errorf("html: cursor (%d,%d) outside %dx%d frame", cursor.Column, cursor.Row, width, height)
	}
	if cursor.Style > 6 {
		return fmt.Errorf("html: invalid cursor style %d", cursor.Style)
	}
	if !cursor.StyleSet && cursor.Style != 0 {
		return fmt.Errorf("html: cursor style must be zero when unset")
	}
	return nil
}

func damageRequiresSnapshot(damage []core.Damage, width, height int) bool {
	for _, item := range damage {
		switch item.Kind {
		case core.DamageText, core.DamageClear:
			if item.X < 0 || item.Y < 0 || item.Width <= 0 || item.Height <= 0 || item.X > width-item.Width || item.Y > height-item.Height || item.Count != 0 {
				return true
			}
		case core.DamageScrollUp, core.DamageFullRedraw:
			return true
		default:
			return true
		}
	}
	return false
}

func (r *Renderer) buildUpdate(frame core.Frame, snapshot bool, cursor Cursor) (Update, error) {
	update := Update{
		SchemaVersion: UpdateSchemaVersion,
		Width:         frame.Width,
		Height:        frame.Height,
		Snapshot:      snapshot,
		Rows:          make([]RowUpdate, 0),
		Styles:        make([]Style, 0),
		Cursor:        cursor,
	}
	styleIDs := make(map[Style]int)
	for y := range frame.Height {
		if !snapshot && rowsEqual(frame.Row(y), r.committed.Row(y)) {
			continue
		}
		if len(update.Rows) >= r.limits.MaxRowsPerUpdate {
			return Update{}, fmt.Errorf("%w: update rows exceed limit %d", ErrLimitExceeded, r.limits.MaxRowsPerUpdate)
		}
		row, err := encodeRow(y, frame.Row(y), &update.Styles, styleIDs, r.limits.MaxStyles)
		if err != nil {
			return Update{}, err
		}
		update.Rows = append(update.Rows, row)
	}
	return update, nil
}

func estimatedUpdateBytes(update Update, limit int) (int, bool) {
	estimate := 256
	add := func(size int) bool {
		if estimate > limit || size > limit-estimate {
			estimate = limit
			return false
		}
		estimate += size
		return true
	}
	if len(update.Styles) > limit/512 || !add(len(update.Styles)*512) {
		return estimate, false
	}
	for _, row := range update.Rows {
		if !add(64) {
			return estimate, false
		}
		for _, cell := range row.Cells {
			if !add(96 + len(cell.Text)*6) {
				return estimate, false
			}
		}
	}
	return estimate, true
}

func rowsEqual(left, right []core.Cell) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !left[i].Equal(right[i]) {
			return false
		}
	}
	return true
}

func encodeRow(y int, cells []core.Cell, styles *[]Style, styleIDs map[Style]int, maxStyles int) (RowUpdate, error) {
	row := RowUpdate{Row: y, Cells: make([]CellUpdate, 0, len(cells))}
	for x := 0; x < len(cells); x++ {
		cell := cells[x]
		if cell.Continuation {
			continue
		}
		style := styleFromCore(cell.Style)
		styleID, ok := styleIDs[style]
		if !ok {
			if len(*styles) >= maxStyles {
				return RowUpdate{}, fmt.Errorf("%w: update styles exceed limit %d", ErrLimitExceeded, maxStyles)
			}
			styleID = len(*styles)
			styleIDs[style] = styleID
			*styles = append(*styles, style)
		}
		width := core.RuneWidth(cell.Rune)
		text := string(cell.Rune)
		if cell.Rune == 0 {
			width = 1
			text = " "
		}
		row.Cells = append(row.Cells, CellUpdate{Column: x, Width: width, Text: text, Style: styleID})
	}
	return row, nil
}

func validateCoreStyle(style core.Style) error {
	const knownAttrs = core.AttrDim | core.AttrUnderline | core.AttrBlink | core.AttrStrikethrough
	if style.Attrs & ^knownAttrs != 0 {
		return fmt.Errorf("unknown style attribute bits %#x", style.Attrs&^knownAttrs)
	}
	if style.UnderlineStyle > core.UnderlineDashed {
		return fmt.Errorf("invalid underline style %d", style.UnderlineStyle)
	}
	if !style.HasForegroundRGB && (style.Foreground < -1 || style.Foreground > 255) {
		return fmt.Errorf("invalid foreground index %d", style.Foreground)
	}
	if !style.HasBackgroundRGB && (style.Background < -1 || style.Background > 255) {
		return fmt.Errorf("invalid background index %d", style.Background)
	}
	if !style.HasUnderlineColorRGB && style.HasUnderlineColor && (style.UnderlineColor < 0 || style.UnderlineColor > 255) {
		return fmt.Errorf("invalid underline color index %d", style.UnderlineColor)
	}
	return nil
}

func styleFromCore(style core.Style) Style {
	return Style{
		Bold:           style.Bold,
		Italic:         style.Italic,
		Inverse:        style.Inverse,
		Dim:            style.Attrs&core.AttrDim != 0,
		Blink:          style.Attrs&core.AttrBlink != 0,
		Strikethrough:  style.Attrs&core.AttrStrikethrough != 0,
		Underline:      style.Attrs&core.AttrUnderline != 0,
		UnderlineStyle: style.UnderlineStyle,
		Foreground:     colorFromCore(style.Foreground, style.HasForegroundRGB, style.ForegroundRGB),
		Background:     colorFromCore(style.Background, style.HasBackgroundRGB, style.BackgroundRGB),
		UnderlineColor: underlineColorFromCore(style),
	}
}

func colorFromCore(index int, hasRGB bool, rgb core.RGB) Color {
	if hasRGB {
		return Color{Kind: ColorRGB, RGB: RGB{R: rgb.R, G: rgb.G, B: rgb.B}}
	}
	if index >= 0 {
		return Color{Kind: ColorIndexed, Index: uint8(index)}
	}
	return Color{Kind: ColorDefault}
}

func underlineColorFromCore(style core.Style) Color {
	if style.HasUnderlineColorRGB {
		return Color{Kind: ColorRGB, RGB: RGB{R: style.UnderlineColorRGB.R, G: style.UnderlineColorRGB.G, B: style.UnderlineColorRGB.B}}
	}
	if style.HasUnderlineColor {
		return Color{Kind: ColorIndexed, Index: uint8(style.UnderlineColor)}
	}
	return Color{Kind: ColorDefault}
}

// Update returns an owned copy that remains valid after later renderer work.
func (p *PreparedDraw) Update() Update {
	if p == nil || p.tx == nil {
		return Update{}
	}
	return cloneUpdate(p.tx.update)
}

// JSON returns an owned canonical JSON representation of Update.
func (p *PreparedDraw) JSON() []byte {
	if p == nil || p.tx == nil {
		return nil
	}
	return append([]byte(nil), p.tx.json...)
}

// Commit atomically advances the renderer shadow exactly once.
func (p *PreparedDraw) Commit() error {
	if p == nil || p.tx == nil || p.tx.renderer == nil {
		return ErrStaleDraw
	}
	tx := p.tx
	if tx.state != transactionPending {
		return ErrFinalizedDraw
	}
	r := tx.renderer
	if tx.generation != r.generation || r.pending != tx {
		return ErrStaleDraw
	}
	r.committed = tx.frame
	r.cursor = tx.cursor
	r.initialized = true
	r.pending = nil
	tx.state = transactionCommitted
	return nil
}

// Abort finalizes the transaction without advancing the renderer shadow.
func (p *PreparedDraw) Abort() error {
	if p == nil || p.tx == nil || p.tx.renderer == nil {
		return ErrStaleDraw
	}
	tx := p.tx
	if tx.state != transactionPending {
		return ErrFinalizedDraw
	}
	r := tx.renderer
	if tx.generation != r.generation || r.pending != tx {
		return ErrStaleDraw
	}
	r.pending = nil
	tx.state = transactionAborted
	return nil
}

// Reset invalidates any prepared draw and clears committed state.
func (r *Renderer) Reset() {
	if r == nil {
		return
	}
	r.generation++
	r.pending = nil
	r.committed = core.Frame{}
	r.cursor = Cursor{}
	r.initialized = false
}
