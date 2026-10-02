package html

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"testing"
	"unicode/utf8"

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

// Invalid UTF-8 never reaches the encoder (cells are validated). Should it,
// the output must stay valid JSON decoding to the same text encoding/json
// would produce, whatever escaping style the toolchain's encoder uses.
func TestAppendJSONStringInvalidUTF8StaysValidJSON(t *testing.T) {
	for _, text := range []string{"\xff", "a\xc3", "\xed\xa0\x80", "\xe2\x80", "ok\xf0\x9f"} {
		want, err := json.Marshal(text)
		require.NoError(t, err)
		var wantText, gotText string
		require.NoError(t, json.Unmarshal(want, &wantText))
		require.NoError(t, json.Unmarshal(appendJSONString(nil, text), &gotText), "%q", text)
		require.Equal(t, wantText, gotText, "%q", text)
	}
}

func FuzzAppendJSONString(f *testing.F) {
	for _, seed := range []string{"", "a<b>&c", "\u2028\u2029", "界😀", "\x00\x1f\x7f"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if !utf8.ValidString(text) {
			t.Skip("cell text is always valid UTF-8")
		}
		want, err := json.Marshal(text)
		require.NoError(t, err)
		require.Equal(t, string(want), string(appendJSONString(nil, text)))
	})
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
	require.ErrorContains(t, err, "invalid color kind 9")
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
	frame.Set(3, 0, core.Cell{Rune: '😀', Style: core.DefaultStyle()})
	frame.Set(4, 0, core.Cell{Continuation: true, Style: core.DefaultStyle()})
	frame.Set(5, 0, core.Cell{Rune: '&', Style: core.DefaultStyle()})
	frame.Set(0, 1, core.Cell{Rune: '"', Style: style})

	renderer, err := New(Options{})
	require.NoError(t, err)
	prepared, err := renderer.Prepare(frame, nil, true, Cursor{Column: 2, Visible: true, Style: 3, StyleSet: true})
	require.NoError(t, err)
	want, err := json.Marshal(prepared.tx.update)
	require.NoError(t, err)
	require.Equal(t, string(want), string(prepared.JSON()))
}

// fillAll sets every field reachable from v to a distinctive non-zero value
// (negative for signed integers) so a field added to the update types without
// a matching encoder change shows up as a JSON mismatch.
func fillAll(t *testing.T, v reflect.Value, colorKind ColorKind, counter *int) {
	t.Helper()
	*counter++
	n := *counter
	if v.Type() == reflect.TypeOf(Color{}) {
		v.FieldByName("Kind").SetUint(uint64(colorKind))
		v.FieldByName("Index").SetUint(uint64(100 + n%100))
		fillAll(t, v.FieldByName("RGB"), colorKind, counter)
		return
	}
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(-int64(n) - 1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(1 + n%120))
	case reflect.String:
		v.SetString("t<&>\"\\界\u2028" + string(rune('a'+n%26)))
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 2, 2))
		for i := range 2 {
			fillAll(t, v.Index(i), colorKind, counter)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			require.True(t, v.Field(i).CanSet(), "%s.%s must be settable", v.Type(), v.Type().Field(i).Name)
			fillAll(t, v.Field(i), colorKind, counter)
		}
	default:
		t.Fatalf("fillAll: unsupported kind %s in %s", v.Kind(), v.Type())
	}
}

func requireNoZeroFields(t *testing.T, v reflect.Value) {
	t.Helper()
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			requireNoZeroFields(t, v.Field(i))
		}
	case reflect.Slice:
		require.NotZero(t, v.Len())
		for i := range v.Len() {
			requireNoZeroFields(t, v.Index(i))
		}
	default:
		require.False(t, v.IsZero(), "zero %s left in filled value", v.Type())
	}
}

// TestEncoderCoversEveryUpdateField fails when a field is added to (or removed
// from) the update types without updating appendUpdateJSON and this test.
func TestEncoderCoversEveryUpdateField(t *testing.T) {
	for typ, fields := range map[reflect.Type]int{
		reflect.TypeOf(Update{}):     7,
		reflect.TypeOf(RowUpdate{}):  2,
		reflect.TypeOf(CellUpdate{}): 4,
		reflect.TypeOf(Style{}):      11,
		reflect.TypeOf(Cursor{}):     5,
		reflect.TypeOf(RGB{}):        3,
		reflect.TypeOf(Color{}):      3,
	} {
		require.Equal(t, fields, typ.NumField(), "%s field count changed: update appendUpdateJSON", typ)
	}

	for _, kind := range []ColorKind{ColorDefault, ColorIndexed, ColorRGB} {
		var update Update
		counter := 0
		fillAll(t, reflect.ValueOf(&update).Elem(), kind, &counter)
		update.Cursor.Style = 6 // keep within the documented DECSCUSR range
		if kind != ColorDefault {
			requireNoZeroFields(t, reflect.ValueOf(update))
		}
		want, err := json.Marshal(update)
		require.NoError(t, err)
		got, err := appendUpdateJSON(nil, update)
		require.NoError(t, err)
		require.Equal(t, string(want), string(got), "color kind %d", kind)
	}
}
