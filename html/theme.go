package html

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bnema/vev-vt/core"
)

// Theme contains generic terminal colors. Consumers can override the same CSS
// custom properties directly when product-specific theme ownership is needed.
type Theme struct {
	Foreground    core.RGB
	Background    core.RGB
	Cursor        core.RGB
	Selection     core.RGB
	SelectionText core.RGB
	Palette       [16]core.RGB
}

// MarshalJSON emits the typed object accepted by the embedded browser
// runtime's setTheme method.
func (theme Theme) MarshalJSON() ([]byte, error) {
	palette := [16]RGB{}
	for index, color := range theme.Palette {
		palette[index] = browserRGB(color)
	}
	return json.Marshal(struct {
		Foreground    RGB     `json:"foreground"`
		Background    RGB     `json:"background"`
		Cursor        RGB     `json:"cursor"`
		Selection     RGB     `json:"selection"`
		SelectionText RGB     `json:"selectionText"`
		Palette       [16]RGB `json:"palette"`
	}{
		Foreground: browserRGB(theme.Foreground), Background: browserRGB(theme.Background),
		Cursor: browserRGB(theme.Cursor), Selection: browserRGB(theme.Selection),
		SelectionText: browserRGB(theme.SelectionText), Palette: palette,
	})
}

func browserRGB(color core.RGB) RGB { return RGB{R: color.R, G: color.G, B: color.B} }

// DefaultTheme returns a neutral dark terminal theme.
func DefaultTheme() Theme {
	return Theme{
		Foreground:    rgb(0xd8, 0xde, 0xe9),
		Background:    rgb(0x10, 0x12, 0x18),
		Cursor:        rgb(0xff, 0x87, 0x00),
		Selection:     rgb(0x39, 0x41, 0x50),
		SelectionText: rgb(0xff, 0xff, 0xff),
		Palette: [16]core.RGB{
			rgb(0x00, 0x00, 0x00), rgb(0x80, 0x00, 0x00), rgb(0x00, 0x80, 0x00), rgb(0x80, 0x80, 0x00),
			rgb(0x00, 0x00, 0x80), rgb(0x80, 0x00, 0x80), rgb(0x00, 0x80, 0x80), rgb(0xc0, 0xc0, 0xc0),
			rgb(0x80, 0x80, 0x80), rgb(0xff, 0x00, 0x00), rgb(0x00, 0xff, 0x00), rgb(0xff, 0xff, 0x00),
			rgb(0x00, 0x00, 0xff), rgb(0xff, 0x00, 0xff), rgb(0x00, 0xff, 0xff), rgb(0xff, 0xff, 0xff),
		},
	}
}

func rgb(r, g, b uint8) core.RGB { return core.RGB{R: r, G: g, B: b} }

// CSS returns deterministic custom-property declarations containing only
// numeric RGB values.
func (theme Theme) CSS() string {
	var builder strings.Builder
	builder.WriteString("--vev-fg:")
	writeRGB(&builder, theme.Foreground)
	builder.WriteString(";--vev-bg:")
	writeRGB(&builder, theme.Background)
	builder.WriteString(";--vev-cursor:")
	writeRGB(&builder, theme.Cursor)
	builder.WriteString(";--vev-selection-bg:")
	writeRGB(&builder, theme.Selection)
	builder.WriteString(";--vev-selection-fg:")
	writeRGB(&builder, theme.SelectionText)
	for index, color := range theme.Palette {
		fmt.Fprintf(&builder, ";--vev-color-%d:", index)
		writeRGB(&builder, color)
	}
	builder.WriteByte(';')
	return builder.String()
}

func writeRGB(builder *strings.Builder, color core.RGB) {
	fmt.Fprintf(builder, "#%02x%02x%02x", color.R, color.G, color.B)
}
