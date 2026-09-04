package ansi

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type nilPointerSource struct{}

func (*nilPointerSource) Columns() int       { panic("nil source method called") }
func (*nilPointerSource) Rows() int          { panic("nil source method called") }
func (*nilPointerSource) Cell(int, int) Cell { panic("nil source method called") }

type nilSliceSource []int

func (nilSliceSource) Columns() int       { panic("nil source method called") }
func (nilSliceSource) Rows() int          { panic("nil source method called") }
func (nilSliceSource) Cell(int, int) Cell { panic("nil source method called") }

type nilMapSource map[int]int

func (nilMapSource) Columns() int       { panic("nil source method called") }
func (nilMapSource) Rows() int          { panic("nil source method called") }
func (nilMapSource) Cell(int, int) Cell { panic("nil source method called") }

type nilFuncSource func()

func (nilFuncSource) Columns() int       { panic("nil source method called") }
func (nilFuncSource) Rows() int          { panic("nil source method called") }
func (nilFuncSource) Cell(int, int) Cell { panic("nil source method called") }

type nilChanSource chan int

func (nilChanSource) Columns() int       { panic("nil source method called") }
func (nilChanSource) Rows() int          { panic("nil source method called") }
func (nilChanSource) Cell(int, int) Cell { panic("nil source method called") }

func TestRendererRejectsTypedNilSourcesBeforeCallingMethods(t *testing.T) {
	for name, source := range map[string]CellSource{
		"interface": nil, "frame": (*Frame)(nil), "pointer": (*nilPointerSource)(nil),
		"slice": nilSliceSource(nil), "map": nilMapSource(nil),
		"function": nilFuncSource(nil), "channel": nilChanSource(nil),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := New(Capabilities{}).Prepare(source, nil, false)
			require.EqualError(t, err, "nil cell source")
		})
	}
}
