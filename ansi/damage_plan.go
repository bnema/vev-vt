package ansi

import (
	"cmp"
	"slices"
)

const maxPlannedDamageSpans = 4096

// buildDamagePlan creates the bounded, canonical view of text and clear damage
// used for terminal emission. The source damage remains untouched because the
// scroll and shadow paths need its original structural information.
func buildDamagePlan(frame CellSource, damage []Damage, skip *Damage) ([]Span, bool) {
	if len(damage) == 1 {
		d := damage[0]
		if skip != nil && sameDamage(d, *skip) {
			return nil, false
		}
		if d.Kind != DamageText && d.Kind != DamageClear {
			return nil, false
		}
		return buildSingleDamageSpans(frame, d)
	}

	spans := make([]Span, 0)
	for _, d := range damage {
		if skip != nil && sameDamage(d, *skip) {
			continue
		}
		if d.Kind != DamageText && d.Kind != DamageClear {
			continue
		}
		x, y, width, height, ok := clampRect(frame, d.X, d.Y, d.Width, d.Height)
		if !ok {
			continue
		}
		for row := y; row < y+height; row++ {
			if len(spans) == maxPlannedDamageSpans {
				return nil, true
			}
			spans = append(spans, wideAlignedSpan(frame, row, x, width))
		}
	}

	slices.SortFunc(spans, func(a, b Span) int {
		if c := cmp.Compare(a.Y, b.Y); c != 0 {
			return c
		}
		return cmp.Compare(a.X, b.X)
	})

	return mergeDamageSpans(spans), false
}

func buildSingleDamageSpans(frame CellSource, d Damage) ([]Span, bool) {
	x, y, width, height, ok := clampRect(frame, d.X, d.Y, d.Width, d.Height)
	if !ok {
		return nil, false
	}
	if height > maxPlannedDamageSpans {
		return nil, true
	}
	spans := make([]Span, height)
	for row := range height {
		spans[row] = wideAlignedSpan(frame, y+row, x, width)
	}
	return spans, false
}

// wideAlignedSpan widens a clamped span so it never splits a wide rune.
// Damage can name only one half of a pair, for example when text damage was
// recorded before a scroll moved a wide rune under it. Writing from the right
// half would erase the rune on the client, and the shadow must hold whole pairs.
func wideAlignedSpan(frame CellSource, y, x, width int) Span {
	if x > 0 && frame.Cell(x, y).Continuation {
		x--
		width++
	}
	if end := x + width; end < frame.Columns() && frame.Cell(end, y).Continuation {
		width++
	}
	return Span{Y: y, X: x, Width: width}
}

func mergeDamageSpans(spans []Span) []Span {
	if len(spans) < 2 {
		return spans
	}

	out := spans[:0]
	for _, span := range spans {
		if len(out) == 0 {
			out = append(out, span)
			continue
		}
		last := &out[len(out)-1]
		// Both endpoints are bounded by frame.Width after clampRect, so these
		// additions cannot overflow. The comparison includes adjacency.
		lastEnd := last.X + last.Width
		spanEnd := span.X + span.Width
		if span.Y != last.Y || span.X > lastEnd {
			out = append(out, span)
			continue
		}
		if spanEnd > lastEnd {
			last.Width = spanEnd - last.X
		}
	}
	return out
}
