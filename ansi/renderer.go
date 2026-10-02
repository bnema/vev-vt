package ansi

import (
	"bytes"
	"strconv"
	"sync"
)

const maxPooledBufferCap = 1 << 20

var bufferPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// Capabilities describes the fixed output features of a Renderer target.
type Capabilities struct {
	SynchronizedOutput bool
}

type Renderer struct {
	caps         Capabilities
	colorProfile ColorProfile
	width        int
	height       int
	hasCommitted bool
	committed    Frame
	// scratch receives the snapshot of the frame being prepared. It never
	// aliases committed or caller storage: a snapshot commit swaps the two so
	// both buffers are reused across draws without allocating.
	scratch Frame
	// generation identifies the latest prepared draw that owns scratch.
	generation uint64
}

// PreparedDraw owns encoded output and its transactional delta until Commit.
// It is returned by value so ordinary prepared draws do not allocate.
type PreparedDraw struct {
	renderer   *Renderer
	candidate  DeltaCandidate
	data       []byte
	generation uint64
	commitOnce *sync.Once
}

func New(caps Capabilities) *Renderer {
	return NewWithColorProfile(caps, ColorProfileTrueColor)
}

// NewWithColorProfile constructs a renderer for a target color profile.
func NewWithColorProfile(caps Capabilities, profile ColorProfile) *Renderer {
	return &Renderer{caps: caps, colorProfile: profile}
}

// Reset forgets the committed shadow and invalidates every outstanding
// prepared draw. The private frame buffers are kept for reuse.
func (r *Renderer) Reset() {
	r.width = 0
	r.height = 0
	r.hasCommitted = false
	r.generation++
}

// Bytes returns the prepared ANSI output. The returned bytes remain valid after
// another draw.
func (p PreparedDraw) Bytes() []byte { return p.data }

// Commit applies the prepared delta exactly once. Discarding it leaves the
// renderer's committed state unchanged.
//
// Committing a draw that a later Prepare has superseded does not apply it:
// the renderer instead drops its committed state, so the next Prepare
// re-emits a full snapshot instead of diffing against a shadow that may no
// longer match the terminal. Draws without output are unaffected.
func (p *PreparedDraw) Commit() {
	if p == nil || p.renderer == nil || p.commitOnce == nil {
		return
	}
	p.commitOnce.Do(func() {
		r := p.renderer
		plan := p.candidate.Plan
		if !plan.Snapshot && plan.Scroll.Height == 0 && len(plan.Spans) == 0 {
			return
		}
		if p.generation != r.generation {
			// A later Prepare or Reset reused the scratch frame this draw's
			// snapshot lives in, so it cannot be applied. Its bytes may
			// nevertheless have reached the terminal, which would then differ
			// from the committed shadow in unknown ways: forget the shadow and
			// invalidate every outstanding draw, so even a delta prepared
			// after this one cannot re-establish a shadow on commit and the
			// next Prepare emits a full snapshot.
			r.hasCommitted = false
			r.generation++
			return
		}
		if !plan.Snapshot && !r.hasCommitted {
			// Unreachable for current draws: Prepare plans a snapshot whenever
			// no shadow exists, and every path that drops the shadow also
			// advances the generation. Keep the shadow dropped defensively.
			return
		}
		if plan.Snapshot {
			// The snapshot lives in the renderer's scratch buffer; promote it
			// and recycle the previous committed buffer as the next scratch.
			r.committed, r.scratch = p.candidate.frame, r.committed
		} else {
			p.candidate.Commit(&r.committed)
		}
		r.width = r.committed.Width
		r.height = r.committed.Height
		r.hasCommitted = true
	})
}

// Prepare plans and encodes a transactional draw. The renderer advances only
// when the returned draw is committed. Keep at most one prepared draw
// outstanding; commit or discard it before calling Prepare again. A draw
// superseded by a later Prepare is not applied: committing it forces the next
// Prepare to emit a full snapshot.
//
// The renderer copies frame into reusable private storage, so the caller may
// keep mutating frame between draws.
func (r *Renderer) Prepare(frame CellSource, damage []Damage, reset bool) (PreparedDraw, error) {
	var candidate DeltaCandidate
	var err error
	if err = validateCellSource(frame); err != nil {
		return PreparedDraw{}, err
	}
	// Any earlier prepared draw loses its claim on the scratch snapshot, even
	// if this Prepare fails after partially rewriting it.
	r.generation++
	columns, rows := frame.Columns(), frame.Rows()
	if !reset && r.hasCommitted && r.width == columns && r.height == rows && len(damage) == 1 && (damage[0].Kind == DamageText || damage[0].Kind == DamageClear) {
		plan := planSingleDamage(frame, damage[0])
		candidate = newDeltaCandidate(frame, plan, &r.scratch)
	} else {
		candidate, err = planDelta(frame, damage, r.committed, reset || !r.hasCommitted, &r.scratch)
	}
	if err != nil {
		return PreparedDraw{}, err
	}
	prepared := PreparedDraw{renderer: r, candidate: candidate, generation: r.generation, commitOnce: new(sync.Once)}
	plan := candidate.Plan
	if !plan.Snapshot && plan.Scroll.Height == 0 && len(plan.Spans) == 0 {
		return prepared, nil
	}

	buf, ok := bufferPool.Get().(*bytes.Buffer)
	if !ok {
		buf = new(bytes.Buffer)
	}
	buf.Reset()
	defer putBuffer(buf)
	if r.caps.SynchronizedOutput {
		buf.WriteString(SyncStartCSI)
	}

	st := newDrawStateForProfile(r.colorProfile)
	if plan.Snapshot {
		if len(plan.Spans) > 0 {
			r.emitDamageSpans(buf, frame, plan.Spans, &st)
		} else {
			r.writeFull(buf, frame, &st)
		}
	} else {
		if plan.Scroll.Height != 0 {
			scroll := plan.Scroll
			kind := DamageScrollUp
			if scroll.Down {
				kind = DamageScrollDown
			}
			emitScroll(buf, Damage{Kind: kind, X: 0, Y: scroll.Y, Width: columns, Height: scroll.Height, Count: scroll.Count})
		}
		for _, span := range plan.Spans {
			r.emitSpan(buf, frame, span.Y, span.X, span.Width, &st)
		}
		buf.WriteString("\x1b[0m")
	}
	if r.caps.SynchronizedOutput {
		buf.WriteString(SyncEndCSI)
	}
	prepared.data = copyBytes(buf)
	return prepared, nil
}

func (r *Renderer) Draw(frame CellSource, damage []Damage) ([]byte, error) {
	prepared, err := r.Prepare(frame, damage, false)
	if err != nil {
		return nil, err
	}
	prepared.Commit()
	return prepared.Bytes(), nil
}

// copyBytes copies the buffer contents into a fresh byte slice and is used
// to return output that is independent of the pooled scratch buffer.
func copyBytes(buf *bytes.Buffer) []byte {
	b := buf.Bytes()
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

func putBuffer(buf *bytes.Buffer) {
	if buf.Cap() > maxPooledBufferCap {
		return
	}
	bufferPool.Put(buf)
}

func needsFull(damage []Damage) bool {
	if len(damage) == 0 {
		return false
	}
	for _, d := range damage {
		if d.Kind == DamageFullRedraw {
			return true
		}
	}
	return false
}

func hasScrollDamage(damage []Damage) bool {
	for _, d := range damage {
		if d.Kind == DamageScrollUp || d.Kind == DamageScrollDown {
			return true
		}
	}
	return false
}

func (r *Renderer) writeFull(out *bytes.Buffer, frame CellSource, st *drawState) {
	columns := frame.Columns()
	for y := range frame.Rows() {
		r.emitSpan(out, frame, y, 0, columns, st)
	}
	out.WriteString("\x1b[0m")
}

func (r *Renderer) emitDamageSpans(out *bytes.Buffer, frame CellSource, spans []Span, st *drawState) {
	for _, span := range spans {
		r.emitSpan(out, frame, span.Y, span.X, span.Width, st)
	}
	out.WriteString("\x1b[0m")
}
func clampRect(frame CellSource, x, y, width, height int) (int, int, int, int, bool) {
	x, width, okX := clampRange(x, width, frame.Columns())
	y, height, okY := clampRange(y, height, frame.Rows())
	if !okX || !okY {
		return 0, 0, 0, 0, false
	}
	return x, y, width, height, true
}

// clampRange intersects [pos, pos+size) with [0, limit) without evaluating an
// overflowing endpoint from untrusted damage coordinates.
func clampRange(pos, size, limit int) (int, int, bool) {
	if size <= 0 || limit <= 0 || pos >= limit {
		return 0, 0, false
	}
	if pos < 0 {
		// -size is safe because size is positive. If pos is at or before that
		// point, the rectangle ends at or before zero.
		if pos <= -size {
			return 0, 0, false
		}
		end := pos + size // pos > -size proves this addition cannot overflow.
		if end > limit {
			end = limit
		}
		return 0, end, true
	}

	available := limit - pos
	return pos, min(size, available), true
}

// writeCursor emits a cursor-positioning CSI sequence without fmt.Fprintf
// allocations. It uses a stack-allocated buffer for integer formatting.
func writeCursor(out *bytes.Buffer, y, x int) {
	out.WriteString("\x1b[")
	var b [16]byte
	n := strconv.AppendInt(b[:0], int64(y+1), 10)
	out.Write(n)
	out.WriteByte(';')
	n = strconv.AppendInt(b[:0], int64(x+1), 10)
	out.Write(n)
	out.WriteByte('H')
}

func sameDamage(a, b Damage) bool {
	return a.Kind == b.Kind && a.X == b.X && a.Y == b.Y && a.Width == b.Width && a.Height == b.Height && a.Count == b.Count
}
