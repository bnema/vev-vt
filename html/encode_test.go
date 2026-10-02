package html

import (
	"encoding/json"
	"math/rand"
	"testing"

	"github.com/bnema/vev-vt/core"
	"github.com/stretchr/testify/require"
)

func TestAppendJSONStringMatchesEncodingJSON(t *testing.T) {
	cases := []string{
		"", " ", "A", `"`, `\`, "<", ">", "&", "\x00", "\x1f", "\x7f", "\b\f\n\r\t",
		"\u2028", "\u2029", "界", "😀", "e\u0301", "a<b>&c\"d\\e\u2028f",
	}
	for r := rune(0); r < 0x300; r++ {
		cases = append(cases, string(r))
	}
	for _, text := range cases {
		want, err := json.Marshal(text)
		require.NoError(t, err)
		require.Equal(t, string(want), string(appendJSONString(nil, text)), "%q", text)
	}
}

// Invalid UTF-8 never reaches the encoder (cells are validated), and the
// escaped form of U+FFFD differs between encoding/json implementations, so
// compare decoded values only.
func TestAppendJSONStringInvalidUTF8DecodesLikeEncodingJSON(t *testing.T) {
	for _, text := range []string{"\xff", "a\xc3", "\xed\xa0\x80"} {
		want, err := json.Marshal(text)
		require.NoError(t, err)
		var wantDecoded, gotDecoded string
		require.NoError(t, json.Unmarshal(want, &wantDecoded))
		require.NoError(t, json.Unmarshal(appendJSONString(nil, text), &gotDecoded))
		require.Equal(t, wantDecoded, gotDecoded)
	}
}

func TestAsciiTextIndexesEveryByte(t *testing.T) {
	require.Len(t, asciiText, 128)
	for i := 0; i < 128; i++ {
		require.Equal(t, byte(i), asciiText[i])
	}
}

func randomColor(rng *rand.Rand) Color {
	switch rng.Intn(3) {
	case 0:
		return Color{Kind: ColorDefault}
	case 1:
		return Color{Kind: ColorIndexed, Index: uint8(rng.Intn(256))}
	}
	return Color{Kind: ColorRGB, RGB: RGB{R: uint8(rng.Intn(256)), G: uint8(rng.Intn(256)), B: uint8(rng.Intn(256))}}
}

func TestAppendUpdateJSONMatchesEncodingJSON(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	texts := []string{" ", "A", "<", "&", `"`, `\`, "界", "😀", "\x01", "\u2028"}
	for range 300 {
		update := Update{
			SchemaVersion: UpdateSchemaVersion,
			Width:         rng.Intn(500),
			Height:        rng.Intn(500),
			Snapshot:      rng.Intn(2) == 0,
			Cursor: Cursor{
				Row: rng.Intn(100), Column: rng.Intn(100), Visible: rng.Intn(2) == 0,
				Style: CursorStyle(rng.Intn(7)), StyleSet: rng.Intn(2) == 0,
			},
		}
		if rng.Intn(5) != 0 {
			update.Rows = make([]RowUpdate, rng.Intn(4))
			for i := range update.Rows {
				update.Rows[i].Row = rng.Intn(1000)
				if rng.Intn(5) != 0 {
					update.Rows[i].Cells = make([]CellUpdate, rng.Intn(5))
					for j := range update.Rows[i].Cells {
						update.Rows[i].Cells[j] = CellUpdate{Column: rng.Intn(300), Width: 1 + rng.Intn(2), Text: texts[rng.Intn(len(texts))], Style: rng.Intn(10)}
					}
				}
			}
		}
		if rng.Intn(5) != 0 {
			update.Styles = make([]Style, rng.Intn(4))
			for i := range update.Styles {
				update.Styles[i] = Style{
					Bold: rng.Intn(2) == 0, Italic: rng.Intn(2) == 0, Inverse: rng.Intn(2) == 0,
					Dim: rng.Intn(2) == 0, Blink: rng.Intn(2) == 0, Strikethrough: rng.Intn(2) == 0,
					Underline: rng.Intn(2) == 0, UnderlineStyle: core.UnderlineStyle(rng.Intn(6)),
					Foreground: randomColor(rng), Background: randomColor(rng), UnderlineColor: randomColor(rng),
				}
			}
		}
		want, err := json.Marshal(update)
		require.NoError(t, err)
		got, err := appendUpdateJSON(nil, update)
		require.NoError(t, err)
		require.Equal(t, string(want), string(got))
	}
}

func TestAppendUpdateJSONRejectsInvalidColorKind(t *testing.T) {
	update := Update{Styles: []Style{{Foreground: Color{Kind: 9}}}}
	_, err := appendUpdateJSON(nil, update)
	require.ErrorContains(t, err, "invalid color kind")
	_, err = json.Marshal(update)
	require.Error(t, err)
}

func TestPreparedJSONMatchesEncodingJSONOfUpdate(t *testing.T) {
	frame := core.NewFrame(6, 2)
	style := core.DefaultStyle()
	style.Bold = true
	style.HasForegroundRGB, style.ForegroundRGB = true, core.RGB{R: 1, G: 2, B: 3}
	style.Background = 200
	frame.Set(0, 0, core.Cell{Rune: '<', Style: style})
	frame.Set(1, 0, core.Cell{Rune: '界', Style: core.DefaultStyle()})
	frame.Set(2, 0, core.Cell{Continuation: true, Style: core.DefaultStyle()})
	frame.Set(4, 1, core.Cell{Rune: '"', Style: style})
	frame.Set(5, 1, core.Cell{Rune: '😀', Style: core.DefaultStyle()})
	// width-2 rune at the last column is invalid; keep it narrow.
	frame.Set(5, 1, core.Cell{Rune: '&', Style: core.DefaultStyle()})

	renderer, err := New(Options{})
	require.NoError(t, err)
	prepared, err := renderer.Prepare(frame, nil, true, Cursor{Column: 2, Visible: true, Style: 3, StyleSet: true})
	require.NoError(t, err)
	want, err := json.Marshal(prepared.tx.update)
	require.NoError(t, err)
	require.Equal(t, string(want), string(prepared.JSON()))
}
