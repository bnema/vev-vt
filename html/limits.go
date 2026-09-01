package html

import (
	"errors"
)

const (
	defaultMaxCells          = 1_000_000
	defaultMaxRowsPerUpdate  = 10_000
	defaultMaxGeneratedBytes = 64 << 20
	defaultMaxStyles         = 65_536
)

var (
	// ErrPendingDraw reports an attempt to prepare while a draw is pending.
	ErrPendingDraw = errors.New("html: prepared draw already pending")
	// ErrFinalizedDraw reports a repeated commit or abort.
	ErrFinalizedDraw = errors.New("html: prepared draw already finalized")
	// ErrStaleDraw reports a nil draw or one invalidated by Reset.
	ErrStaleDraw = errors.New("html: prepared draw is stale")
	// ErrLimitExceeded reports a configured renderer bound violation.
	ErrLimitExceeded = errors.New("html: resource limit exceeded")
	// ErrInvalidLimits reports a negative limit field.
	ErrInvalidLimits = errors.New("html: limits must not be negative")
)

// Limits bounds renderer work and retained update data. Zero values select
// documented safe defaults.
type Limits struct {
	MaxCells          int
	MaxRowsPerUpdate  int
	MaxGeneratedBytes int
	MaxStyles         int
}

// DefaultLimits returns the bounds used for zero-valued limit fields.
func DefaultLimits() Limits {
	return Limits{
		MaxCells:          defaultMaxCells,
		MaxRowsPerUpdate:  defaultMaxRowsPerUpdate,
		MaxGeneratedBytes: defaultMaxGeneratedBytes,
		MaxStyles:         defaultMaxStyles,
	}
}

// Options configures a Renderer for its lifetime.
type Options struct {
	Limits Limits
}

func normalizeLimits(limits Limits) (Limits, error) {
	if limits.MaxCells < 0 || limits.MaxRowsPerUpdate < 0 || limits.MaxGeneratedBytes < 0 || limits.MaxStyles < 0 {
		return Limits{}, ErrInvalidLimits
	}
	defaults := DefaultLimits()
	if limits.MaxCells == 0 {
		limits.MaxCells = defaults.MaxCells
	}
	if limits.MaxRowsPerUpdate == 0 {
		limits.MaxRowsPerUpdate = defaults.MaxRowsPerUpdate
	}
	if limits.MaxGeneratedBytes == 0 {
		limits.MaxGeneratedBytes = defaults.MaxGeneratedBytes
	}
	if limits.MaxStyles == 0 {
		limits.MaxStyles = defaults.MaxStyles
	}
	return limits, nil
}
