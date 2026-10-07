package vt

import (
	"iter"
	"slices"

	renderer "github.com/bnema/vev-vt/core"
)

// styleSetInline is the number of distinct styles kept in a linear slice
// before the set switches to a map. Typical rows use a handful of styles, where
// a short compare loop beats hashing the 56-byte Style key.
const styleSetInline = 16

// styleSet is a reusable set of canonical styles for one history row. It keeps
// small sets in a slice and only builds a map for high-cardinality rows. The
// zero value is empty and ready to use.
type styleSet struct {
	small []renderer.Style
	large map[renderer.Style]struct{}
}

func (s *styleSet) reset() {
	s.small = s.small[:0]
	if len(s.large) > maxRetainedStyleScratch {
		s.large = nil
	} else {
		clear(s.large)
	}
}

func (s *styleSet) add(style renderer.Style) {
	if len(s.large) > 0 {
		s.large[style] = struct{}{}
		return
	}
	if slices.Contains(s.small, style) {
		return
	}
	if len(s.small) < styleSetInline {
		s.small = append(s.small, style)
		return
	}
	if s.large == nil {
		s.large = make(map[renderer.Style]struct{}, 2*styleSetInline)
	}
	for _, existing := range s.small {
		s.large[existing] = struct{}{}
	}
	s.large[style] = struct{}{}
	s.small = s.small[:0]
}

func (s *styleSet) len() int {
	if len(s.large) > 0 {
		return len(s.large)
	}
	return len(s.small)
}

// all yields each style once, in unspecified order.
func (s *styleSet) all() iter.Seq[renderer.Style] {
	return func(yield func(renderer.Style) bool) {
		if len(s.large) > 0 {
			for style := range s.large {
				if !yield(style) {
					return
				}
			}
			return
		}
		for _, style := range s.small {
			if !yield(style) {
				return
			}
		}
	}
}
