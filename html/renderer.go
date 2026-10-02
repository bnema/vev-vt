package html

import (
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
	// Encoder scratch reused across updates; never referenced by an Update.
	ends      []int
	text      []byte
	rowStarts []int
}

// CellSource is the read-only semantic grid consumed by HTML rendering.
type CellSource = core.CellSource

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
func (r *Renderer) Prepare(source CellSource, damage []core.Damage, reset bool, cursor Cursor) (*PreparedDraw, error) {
	if r == nil {
		return nil, fmt.Errorf("html: nil renderer")
	}
	if r.pending != nil {
		return nil, ErrPendingDraw
	}
	frame, err := materializeCellSource(source)
	if err != nil {
		return nil, err
	}
	scratch, err := r.validateFrame(frame)
	if err != nil {
		return nil, err
	}
	cursor = normalizeWrapPendingCursor(cursor, frame.Width)
	if err := validateCursor(cursor, frame.Width, frame.Height); err != nil {
		return nil, err
	}

	snapshot := reset || !r.initialized || r.committed.Width != frame.Width || r.committed.Height != frame.Height || damageRequiresSnapshot(damage, frame.Width, frame.Height)
	update, err := r.buildUpdate(frame, scratch, snapshot, cursor)
	if err != nil {
		return nil, err
	}
	encoded, err := encodeBoundedUpdate(update, r.limits.MaxGeneratedBytes)
	if err != nil {
		return nil, err
	}

	tx := &transaction{
		renderer:   r,
		generation: r.generation,
		state:      transactionPending,
		update:     update,
		json:       encoded,
		frame:      frame,
		cursor:     cursor,
	}
	r.pending = tx
	return &PreparedDraw{tx: tx}, nil
}

// validateFrame checks limits and cell invariants. It returns a one-row
// scratch buffer that the caller may reuse for the rest of this Prepare.
func (r *Renderer) validateFrame(frame core.Frame) ([]core.Cell, error) {
	if frame.Width <= 0 || frame.Height <= 0 {
		return nil, fmt.Errorf("html: invalid frame size %dx%d", frame.Width, frame.Height)
	}
	if frame.Width > math.MaxInt/frame.Height {
		return nil, fmt.Errorf("%w: frame cell count overflows int", ErrLimitExceeded)
	}
	cells := frame.Width * frame.Height
	if cells > r.limits.MaxCells {
		return nil, fmt.Errorf("%w: frame has %d cells, limit is %d", ErrLimitExceeded, cells, r.limits.MaxCells)
	}
	if frame.Height > r.limits.MaxRowsPerUpdate {
		return nil, fmt.Errorf("%w: frame has %d rows, limit is %d", ErrLimitExceeded, frame.Height, r.limits.MaxRowsPerUpdate)
	}
	if err := frame.Validate(); err != nil {
		return nil, fmt.Errorf("html: validate frame: %w", err)
	}
	scratch := make([]core.Cell, frame.Width)
	for y := range frame.Height {
		row := readRow(frame, y, scratch)
		for x := 0; x < frame.Width; x++ {
			cell := row[x]
			if err := validateCoreStyle(cell.Style); err != nil {
				return nil, fmt.Errorf("html: cell (%d,%d): %w", x, y, err)
			}
			if cell.Continuation {
				if cell.Rune != 0 {
					return nil, fmt.Errorf("html: cell (%d,%d): wide continuation contains a rune", x, y)
				}
				if x == 0 || row[x-1].Continuation || core.RuneWidth(row[x-1].Rune) != 2 {
					return nil, fmt.Errorf("html: cell (%d,%d): orphan wide continuation", x, y)
				}
				if !cell.Style.Equal(row[x-1].Style) {
					return nil, fmt.Errorf("html: cell (%d,%d): wide continuation style differs from its head", x, y)
				}
				continue
			}
			if cell.Rune == 0 {
				continue
			}
			if !utf8.ValidRune(cell.Rune) {
				return nil, fmt.Errorf("html: cell (%d,%d): invalid Unicode scalar", x, y)
			}
			width := core.RuneWidth(cell.Rune)
			switch width {
			case 1:
			case 2:
				if x+1 >= frame.Width || !row[x+1].Continuation {
					return nil, fmt.Errorf("html: cell (%d,%d): wide rune lacks continuation", x, y)
				}
			case 0:
				return nil, fmt.Errorf("html: cell (%d,%d): unsupported zero-width rune", x, y)
			default:
				return nil, fmt.Errorf("html: cell (%d,%d): unsupported rune width %d", x, y, width)
			}
		}
	}
	return scratch, nil
}

// materializeCellSource reads a source into an owned frame. A core.Frame
// takes the fast clone path; any other source is copied cell by cell.
func materializeCellSource(source CellSource) (core.Frame, error) {
	if source == nil {
		return core.Frame{}, fmt.Errorf("html: nil cell source")
	}
	switch frame := source.(type) {
	case core.Frame:
		if err := frame.Validate(); err != nil {
			return core.Frame{}, fmt.Errorf("html: validate frame: %w", err)
		}
		return frame.Clone(), nil
	case *core.Frame:
		if frame == nil {
			return core.Frame{}, fmt.Errorf("html: nil cell source")
		}
		if err := frame.Validate(); err != nil {
			return core.Frame{}, fmt.Errorf("html: validate frame: %w", err)
		}
		return frame.Clone(), nil
	}
	width, height := source.Columns(), source.Rows()
	if width <= 0 || height <= 0 {
		return core.Frame{}, fmt.Errorf("html: invalid cell source size %dx%d", width, height)
	}
	clone := core.NewFrame(width, height)
	for y := range height {
		for x := range width {
			clone.Set(x, y, source.Cell(x, y))
		}
	}
	return clone, nil
}

// normalizeWrapPendingCursor maps the deferred-wrap one-past-end column
// produced after writing the last column with autowrap enabled onto the
// last visible column. It leaves every in-bounds cursor untouched.
func normalizeWrapPendingCursor(cursor Cursor, width int) Cursor {
	if cursor.Column == width && cursor.Row >= 0 {
		cursor.Column = width - 1
	}
	return cursor
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
			if item.X < 0 || item.Y < 0 || item.Width <= 0 || item.Height <= 0 || item.X > width-item.Width || item.Y > height-item.Height {
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

// readRow copies logical row y into buf and returns the filled prefix. It is
// the allocation-free equivalent of core.Frame.Row.
func readRow(frame core.Frame, y int, buf []core.Cell) []core.Cell {
	row := buf[:frame.Width]
	for x := range row {
		row[x] = frame.Cell(x, y)
	}
	return row
}

// rowEqualsFrame reports whether row matches logical row y of frame, stopping
// at the first difference without materializing the frame row.
func rowEqualsFrame(row []core.Cell, frame core.Frame, y int) bool {
	if len(row) != frame.Width {
		return false
	}
	for x := range row {
		if !row[x].Equal(frame.Cell(x, y)) {
			return false
		}
	}
	return true
}

func (r *Renderer) buildUpdate(frame core.Frame, scratch []core.Cell, snapshot bool, cursor Cursor) (Update, error) {
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
	// Every row appends its runs to cells and their text to text; ends[i]
	// is the text offset after cells[i]. Rows and texts are sliced out once
	// at the end, so an update costs one cell slice and one string.
	// cells is owned by the update and sized for one full row (an
	// incremental update usually changes one or a few rows). The other
	// buffers are renderer scratch: the text is copied into one string.
	cells := make([]CellUpdate, 0, frame.Width)
	ends, text, rowStarts := r.ends[:0], r.text[:0], r.rowStarts[:0]
	if cap(ends) < frame.Width {
		ends = make([]int, 0, frame.Width)
		text = make([]byte, 0, frame.Width)
		rowStarts = make([]int, 0, 4)
	}
	defer func() {
		// Keep scratch for frames up to the common 240x80 size only, so one
		// huge snapshot does not pin its buffers for the renderer lifetime.
		const maxRetainedCells = 240 * 80
		if cap(ends) <= maxRetainedCells && cap(text) <= 4*maxRetainedCells {
			r.ends, r.text, r.rowStarts = ends, text, rowStarts
		}
	}()
	for y := range frame.Height {
		row := readRow(frame, y, scratch)
		if !snapshot && rowEqualsFrame(row, r.committed, y) {
			continue
		}
		if len(update.Rows) >= r.limits.MaxRowsPerUpdate {
			return Update{}, fmt.Errorf("%w: update rows exceed limit %d", ErrLimitExceeded, r.limits.MaxRowsPerUpdate)
		}
		rowStarts = append(rowStarts, len(cells))
		update.Rows = append(update.Rows, RowUpdate{Row: y})
		var err error
		cells, ends, text, err = encodeRow(row, cells, ends, text, &update.Styles, styleIDs, r.limits.MaxStyles)
		if err != nil {
			return Update{}, err
		}
	}
	all := string(text)
	offset := 0
	for i := range cells {
		cells[i].Text = all[offset:ends[i]]
		offset = ends[i]
	}
	for i := range update.Rows {
		next := len(cells)
		if i+1 < len(rowStarts) {
			next = rowStarts[i+1]
		}
		update.Rows[i].Cells = cells[rowStarts[i]:next:next]
	}
	return update, nil
}

// encodeBoundedUpdate encodes one update and enforces MaxGeneratedBytes on
// the exact encoded size. The previous conservative estimate could reject
// compact updates well below the configured limit.
func encodeBoundedUpdate(update Update, limit int) ([]byte, error) {
	// The hint never exceeds limit, so an oversized update cannot reserve its
	// full estimate before the limit rejects it. limit+1 would overflow for
	// math.MaxInt.
	encoded, err := appendUpdateJSON(make([]byte, 0, min(estimateUpdateJSONSize(update), limit)), update)
	if err != nil {
		return nil, fmt.Errorf("html: encode update: %w", err)
	}
	if len(encoded) > limit {
		return nil, fmt.Errorf("%w: generated update is %d bytes, limit is %d", ErrLimitExceeded, len(encoded), limit)
	}
	return encoded, nil
}

// runText returns the single-column printable ASCII text of cell, treating a
// blank cell as a space, or false when the cell cannot join a text run.
func runText(cell core.Cell) (byte, bool) {
	switch {
	case cell.Continuation:
		return 0, false
	case cell.Rune == 0:
		return ' ', true
	case cell.Rune >= 0x20 && cell.Rune < 0x7f:
		return byte(cell.Rune), true
	default:
		return 0, false
	}
}

// encodeRow appends one CellUpdate per text run of a row to cells, its text
// to text, and the text end offset to ends. Adjacent printable ASCII or blank
// cells with equal styles merge into one run whose Width equals its byte
// length. Every other cell (wide or non-ASCII) stays a single entry, so
// browsers never rely on a fallback font's advance for column alignment.
// Text fields are filled by the caller.
func encodeRow(row []core.Cell, cells []CellUpdate, ends []int, text []byte, styles *[]Style, styleIDs map[Style]int, maxStyles int) ([]CellUpdate, []int, []byte, error) {
	for x := 0; x < len(row); x++ {
		cell := row[x]
		if cell.Continuation {
			continue
		}
		style := styleFromCore(cell.Style)
		styleID, ok := styleIDs[style]
		if !ok {
			if len(*styles) >= maxStyles {
				return cells, ends, text, fmt.Errorf("%w: update styles exceed limit %d", ErrLimitExceeded, maxStyles)
			}
			styleID = len(*styles)
			styleIDs[style] = styleID
			*styles = append(*styles, style)
		}
		if first, ok := runText(cell); ok {
			start := x
			text = append(text, first)
			for x+1 < len(row) && row[x+1].Style.Equal(cell.Style) {
				next, ok := runText(row[x+1])
				if !ok {
					break
				}
				text = append(text, next)
				x++
			}
			cells = append(cells, CellUpdate{Column: start, Width: x - start + 1, Style: styleID})
		} else {
			text = utf8.AppendRune(text, cell.Rune)
			cells = append(cells, CellUpdate{Column: x, Width: core.RuneWidth(cell.Rune), Style: styleID})
		}
		ends = append(ends, len(text))
	}
	return cells, ends, text, nil
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

// Prepare validates every indexed color before these uint8 conversions.
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
