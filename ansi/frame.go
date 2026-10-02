package ansi

import (
	"fmt"
	"reflect"

	"github.com/bnema/vev-vt/core"
)

// Frame is an alias for the frontend-neutral terminal grid. ANSI rendering
// consumes it but does not own its model or storage policy.
type Frame = core.Frame

// CellSource is the read-only semantic grid consumed by ANSI rendering.
type CellSource = core.CellSource

var NewFrame = core.NewFrame

func validateCellSource(source CellSource) error {
	if source == nil {
		return fmt.Errorf("nil cell source")
	}
	value := reflect.ValueOf(source)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return fmt.Errorf("nil cell source")
		}
	}
	if source.Columns() <= 0 || source.Rows() <= 0 {
		return fmt.Errorf("invalid cell source size %dx%d", source.Columns(), source.Rows())
	}
	switch frame := source.(type) {
	case Frame:
		return frame.Validate()
	case *Frame:
		return frame.Validate()
	}
	return nil
}

// snapshotCellSource returns an independent Frame holding source's cells. When
// scratch is non-nil the snapshot is written into scratch's existing storage
// (allocating only to grow or on first use) and the returned Frame shares that
// storage; the caller owns scratch and must not hand it to anyone else while
// the snapshot is live. A nil scratch allocates a fresh frame.
func snapshotCellSource(scratch *Frame, source CellSource) Frame {
	if scratch == nil {
		return cloneCellSource(source)
	}
	switch frame := source.(type) {
	case Frame:
		scratch.CopyFrom(frame)
		return *scratch
	case *Frame:
		scratch.CopyFrom(*frame)
		return *scratch
	}
	columns, rows := source.Columns(), source.Rows()
	if scratch.Width != columns || scratch.Height != rows || scratch.Validate() != nil {
		*scratch = NewFrame(columns, rows)
	}
	for y := range rows {
		for x := range columns {
			scratch.Set(x, y, source.Cell(x, y))
		}
	}
	return *scratch
}

// compactFrames reports whether both sources are core.Frame values (directly or
// by pointer), enabling stored-cell comparison without the CellSource interface.
type framePair struct{ a, b Frame }

func compactFrames(a, b CellSource) (framePair, bool) {
	fa, ok := asFrame(a)
	if !ok {
		return framePair{}, false
	}
	fb, ok := asFrame(b)
	if !ok {
		return framePair{}, false
	}
	return framePair{fa, fb}, true
}

func asFrame(source CellSource) (Frame, bool) {
	switch frame := source.(type) {
	case Frame:
		return frame, true
	case *Frame:
		if frame != nil {
			return *frame, true
		}
	}
	return Frame{}, false
}

func cloneCellSource(source CellSource) Frame {
	switch frame := source.(type) {
	case Frame:
		return frame.Clone()
	case *Frame:
		return frame.Clone()
	}
	clone := NewFrame(source.Columns(), source.Rows())
	for y := range source.Rows() {
		for x := range source.Columns() {
			clone.Set(x, y, source.Cell(x, y))
		}
	}
	return clone
}
