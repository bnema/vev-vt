package ansi

// Span identifies a changed horizontal range in a logical frame row.
type Span struct {
	Y     int
	X     int
	Width int
}

// Scroll identifies a safe full-width scroll.
type Scroll struct {
	Y      int
	Height int
	Count  int
	Down   bool
}

// DeltaPlan describes how to advance a committed frame to the candidate frame.
// Snapshot requests replacement of the committed frame; Spans, when present,
// are the canonical ranges to encode before that replacement.
type DeltaPlan struct {
	Snapshot bool
	Scroll   Scroll
	Spans    []Span
}

// DeltaCandidate holds a plan and owns a source frame snapshot when the plan
// has work to commit. Empty candidates are true no-ops.
type DeltaCandidate struct {
	Plan  DeltaPlan
	frame Frame
}

func newDeltaCandidate(frame CellSource, plan DeltaPlan) DeltaCandidate {
	candidate := DeltaCandidate{Plan: plan}
	if plan.Snapshot || plan.Scroll.Height != 0 || len(plan.Spans) != 0 {
		candidate.frame = cloneCellSource(frame)
	}
	return candidate
}

// PlanDelta prepares a non-mutating update from committed to frame.
func PlanDelta(frame CellSource, damage []Damage, committed Frame, reset bool) (DeltaCandidate, error) {
	if err := validateCellSource(frame); err != nil {
		return DeltaCandidate{}, err
	}

	if reset || frame.Columns() != committed.Columns() || frame.Rows() != committed.Rows() {
		return newDeltaCandidate(frame, DeltaPlan{Snapshot: true}), nil
	}
	if err := committed.Validate(); err != nil {
		return DeltaCandidate{}, err
	}

	if len(damage) == 1 && (damage[0].Kind == DamageText || damage[0].Kind == DamageClear) {
		return newDeltaCandidate(frame, planSingleDamage(frame, damage[0])), nil
	}

	if needsFull(damage) {
		dirty, full := buildDirtyLinePlan(frame, committed)
		if len(dirty) > 0 || full {
			return newDeltaCandidate(frame, DeltaPlan{Snapshot: true, Spans: dirty}), nil
		}
		return DeltaCandidate{}, nil
	}

	var plan DeltaPlan
	var skip *Damage
	if scroll, ok := findSafeScroll(frame, damage); ok && canApplyScrollAgainst(frame, scroll, damage, committed) {
		plan.Scroll = Scroll{Y: scroll.Y, Height: scroll.Height, Count: scroll.Count, Down: scroll.Kind == DamageScrollDown}
		skip = &scroll
	} else if hasScrollDamage(damage) {
		return newDeltaCandidate(frame, DeltaPlan{Snapshot: true}), nil
	}

	var spans []Span
	var full bool
	if len(damage) == 0 {
		spans, full = buildDirtyLinePlan(frame, committed)
	} else {
		spans, full = buildDamagePlan(frame, damage, skip)
	}
	if full {
		return newDeltaCandidate(frame, DeltaPlan{Snapshot: true}), nil
	}
	if deltaCostsSnapshot(frame, spans, plan.Scroll.Height != 0) {
		if plan.Scroll.Height != 0 {
			return newDeltaCandidate(frame, DeltaPlan{Snapshot: true}), nil
		}
		return newDeltaCandidate(frame, DeltaPlan{Snapshot: true, Spans: spans}), nil
	}
	plan.Spans = spans
	return newDeltaCandidate(frame, plan), nil
}

func planSingleDamage(frame CellSource, d Damage) DeltaPlan {
	spans, full := buildSingleDamageSpans(frame, d)
	if full {
		return DeltaPlan{Snapshot: true}
	}
	plan := DeltaPlan{Spans: spans}
	if deltaCostsSnapshot(frame, spans, false) {
		plan.Snapshot = true
	}
	return plan
}

func buildDirtyLinePlan(frame, committed CellSource) ([]Span, bool) {
	var spans []Span
	for y := range frame.Rows() {
		dirty := false
		for x := range frame.Columns() {
			if !frame.Cell(x, y).Equal(committed.Cell(x, y)) {
				dirty = true
				break
			}
		}
		if dirty {
			if len(spans) == maxPlannedDamageSpans {
				return nil, true
			}
			spans = append(spans, Span{Y: y, Width: frame.Columns()})
		}
	}
	return spans, false
}

func deltaCostsSnapshot(frame CellSource, spans []Span, hasScroll bool) bool {
	if !hasScroll && frame.Rows() > 1 && len(spans) == 1 {
		return false
	}

	var deltaCost int64
	if hasScroll {
		deltaCost = 6
	}
	for _, span := range spans {
		deltaCost += 12 + int64(span.Width)
	}

	minimumSnapshotCost := int64(frame.Rows()) * (12 + int64(frame.Columns()))
	if deltaCost < minimumSnapshotCost {
		return false
	}
	maximumSnapshotCost := int64(frame.Rows()) * (8 + 5*int64(frame.Columns()))
	if deltaCost >= maximumSnapshotCost {
		return true
	}
	if !hasScroll && fullWidthRows(frame, spans) {
		return true
	}

	for _, span := range spans {
		deltaCost += 4 * int64(styleRunCountIn(frame, span.Y, span.X, span.Width)-1)
	}
	var snapshotCost int64
	for y := range frame.Rows() {
		snapshotCost += spanCost(frame, y, 0, frame.Columns())
	}
	return deltaCost >= snapshotCost
}

func fullWidthRows(frame CellSource, spans []Span) bool {
	if len(spans) != frame.Rows() {
		return false
	}
	for y, span := range spans {
		if span.Y != y || span.X != 0 || span.Width != frame.Columns() {
			return false
		}
	}
	return true
}

func spanCost(frame CellSource, y, x, width int) int64 {
	return 8 + int64(width) + 4*int64(styleRunCountIn(frame, y, x, width))
}

func styleRunCountIn(frame CellSource, y, x, width int) int {
	if width == 0 {
		return 0
	}
	runs := 1
	previous := frame.Cell(x, y).Style
	for column := x + 1; column < x+width; column++ {
		style := frame.Cell(column, y).Style
		if previous != style && !previous.Equal(style) {
			runs++
		}
		previous = style
	}
	return runs
}

// Commit advances dst to the frame snapshot owned by the candidate.
func (c DeltaCandidate) Commit(dst *Frame) {
	if !c.Plan.Snapshot && c.Plan.Scroll.Height == 0 && len(c.Plan.Spans) == 0 {
		return
	}
	if c.Plan.Snapshot || dst.Width != c.frame.Width || dst.Height != c.frame.Height {
		replaceFrame(dst, c.frame)
		return
	}
	if c.Plan.Scroll.Height != 0 {
		scroll := c.Plan.Scroll
		if scroll.Down {
			dst.ScrollDown(scroll.Y, scroll.Y+scroll.Height-1, scroll.Count)
		} else {
			dst.ScrollUp(scroll.Y, scroll.Y+scroll.Height-1, scroll.Count)
		}
	}
	for _, span := range c.Plan.Spans {
		for x := span.X; x < span.X+span.Width; x++ {
			dst.Set(x, span.Y, c.frame.Cell(x, span.Y))
		}
	}
}

func replaceFrame(dst *Frame, src Frame) {
	dst.Replace(src)
}
