package browser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeEventValidatesTheClosedSchema(t *testing.T) {
	event, err := DecodeEvent([]byte(`{"schemaVersion":1,"type":"text","text":"é"}`), EventLimits{})
	require.NoError(t, err)
	require.Equal(t, EventText, event.Kind)
	require.Equal(t, "é", event.Text.Text)

	_, err = DecodeEvent([]byte(`{"schemaVersion":1,"type":"text","text":"x","unknown":true}`), EventLimits{})
	require.Error(t, err)

	_, err = DecodeEvent([]byte(`{"schemaVersion":1,"type":"paste","text":"toolong"}`), EventLimits{MaxPasteBytes: 3})
	require.ErrorIs(t, err, ErrEventLimit)

	_, err = DecodeEvent([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}, EventLimits{})
	require.ErrorContains(t, err, "not valid UTF-8")

	_, err = DecodeEvent([]byte(`{"schemaVersion":1,"type":"resize","columns":80,"rows":24,"pixelWidth":800,"pixelHeight":480,"cellWidth":10,"cellHeight":20,"devicePixelRatio":1}`), EventLimits{})
	require.NoError(t, err)
}

func TestDecodeEventAcceptsRuntimeEventKinds(t *testing.T) {
	tests := []struct {
		kind EventKind
		json string
	}{
		{EventKey, `{"schemaVersion":1,"type":"key","key":"ArrowUp","code":"ArrowUp","alt":false,"ctrl":false,"meta":false,"shift":false,"repeat":false,"location":0}`},
		{EventPaste, `{"schemaVersion":1,"type":"paste","text":"hello"}`},
		{EventPointer, `{"schemaVersion":1,"type":"pointer","action":"down","button":0,"buttons":1,"row":0,"column":0,"x":1,"y":2,"alt":false,"ctrl":false,"meta":false,"shift":false}`},
		{EventWheel, `{"schemaVersion":1,"type":"wheel","deltaX":1,"deltaY":2,"deltaMode":0,"row":0,"column":0,"alt":false,"ctrl":false,"meta":false,"shift":false}`},
		{EventFocus, `{"schemaVersion":1,"type":"focus","focused":true}`},
	}
	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			event, err := DecodeEvent([]byte(test.json), EventLimits{})
			require.NoError(t, err)
			require.Equal(t, test.kind, event.Kind)
		})
	}
}

func FuzzDecodeEvent(f *testing.F) {
	f.Add([]byte(`{"schemaVersion":1,"type":"focus","focused":true}`))
	f.Add([]byte(`{"schemaVersion":1,"type":"text","text":"hello"}`))
	f.Add([]byte(`not json`))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeEvent(data, EventLimits{MaxEventBytes: 4 << 10})
	})
}
