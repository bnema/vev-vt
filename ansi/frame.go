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
