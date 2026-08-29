package html

import (
	"encoding/json"
	"fmt"

	"github.com/bnema/vev-vt/core"
)

// UpdateSchemaVersion identifies the browser update contract.
const UpdateSchemaVersion uint16 = 1

// CursorStyle is a DECSCUSR cursor shape value from 0 through 6.
type CursorStyle uint8

// Cursor is the absolute cursor state associated with an update. StyleSet
// distinguishes an unset style from an explicit DECSCUSR style zero.
type Cursor struct {
	Row      int         `json:"row"`
	Column   int         `json:"column"`
	Visible  bool        `json:"visible"`
	Style    CursorStyle `json:"style"`
	StyleSet bool        `json:"styleSet"`
}

// ColorKind identifies how a terminal color is resolved.
type ColorKind uint8

const (
	ColorDefault ColorKind = iota
	ColorIndexed
	ColorRGB
)

// RGB is a browser-facing RGB color.
type RGB struct {
	R uint8 `json:"r"`
	G uint8 `json:"g"`
	B uint8 `json:"b"`
}

// Color is a validated terminal color token.
type Color struct {
	Kind  ColorKind
	Index uint8
	RGB   RGB
}

func (color Color) MarshalJSON() ([]byte, error) {
	switch color.Kind {
	case ColorDefault:
		return []byte(`{"kind":0}`), nil
	case ColorIndexed:
		return json.Marshal(struct {
			Kind  ColorKind `json:"kind"`
			Index uint8     `json:"index"`
		}{Kind: color.Kind, Index: color.Index})
	case ColorRGB:
		return json.Marshal(struct {
			Kind ColorKind `json:"kind"`
			RGB  RGB       `json:"rgb"`
		}{Kind: color.Kind, RGB: color.RGB})
	default:
		return nil, fmt.Errorf("html: invalid color kind %d", color.Kind)
	}
}

// Style is the canonical, browser-facing subset of core.Style.
type Style struct {
	Bold           bool                `json:"bold,omitempty"`
	Italic         bool                `json:"italic,omitempty"`
	Inverse        bool                `json:"inverse,omitempty"`
	Dim            bool                `json:"dim,omitempty"`
	Blink          bool                `json:"blink,omitempty"`
	Strikethrough  bool                `json:"strikethrough,omitempty"`
	Underline      bool                `json:"underline,omitempty"`
	UnderlineStyle core.UnderlineStyle `json:"underlineStyle,omitempty"`
	Foreground     Color               `json:"foreground"`
	Background     Color               `json:"background"`
	UnderlineColor Color               `json:"underlineColor"`
}

// CellUpdate occupies Width terminal columns beginning at Column. Wide-cell
// continuation markers are represented by the preceding cell's width.
type CellUpdate struct {
	Column int    `json:"column"`
	Width  int    `json:"width"`
	Text   string `json:"text"`
	Style  int    `json:"style"`
}

// RowUpdate replaces one complete logical terminal row.
type RowUpdate struct {
	Row   int          `json:"row"`
	Cells []CellUpdate `json:"cells"`
}

// Update is an immutable-by-contract browser update. Snapshot updates contain
// every logical row; incremental updates contain complete changed rows.
type Update struct {
	SchemaVersion uint16      `json:"schemaVersion"`
	Width         int         `json:"width"`
	Height        int         `json:"height"`
	Snapshot      bool        `json:"snapshot"`
	Rows          []RowUpdate `json:"rows"`
	Styles        []Style     `json:"styles"`
	Cursor        Cursor      `json:"cursor"`
}

func cloneUpdate(src Update) Update {
	dst := src
	dst.Styles = make([]Style, len(src.Styles))
	copy(dst.Styles, src.Styles)
	dst.Rows = make([]RowUpdate, len(src.Rows))
	for i := range src.Rows {
		dst.Rows[i] = src.Rows[i]
		dst.Rows[i].Cells = append([]CellUpdate(nil), src.Rows[i].Cells...)
	}
	return dst
}
