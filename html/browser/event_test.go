package browser

import (
	"encoding/json"
	"strings"
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
			if test.kind == EventKey {
				require.Equal(t, "ArrowUp", event.Key.Key)
			}
		})
	}
}

func TestDefaultEventLimitsAcceptEscapedMaximumPaste(t *testing.T) {
	limits := DefaultEventLimits()
	data, err := json.Marshal(textWire{
		SchemaVersion: EventSchemaVersion,
		Kind:          EventPaste,
		Text:          strings.Repeat("\x00", limits.MaxPasteBytes),
	})
	require.NoError(t, err)

	event, err := DecodeEvent(data, EventLimits{})
	require.NoError(t, err)
	require.NotNil(t, event.Paste)
	require.Len(t, event.Paste.Text, limits.MaxPasteBytes)
}

func TestNormalizeEventLimitsRejectsImpossibleEnvelope(t *testing.T) {
	_, err := normalizeEventLimits(EventLimits{MaxEventBytes: 69, MaxTextBytes: 1, MaxPasteBytes: 1})
	require.ErrorIs(t, err, ErrInvalidEventLimits)
}

func TestDecodeEventRejectsInvalidRuntimePayloads(t *testing.T) {
	tests := map[string]string{
		"schema":                `{"schemaVersion":2,"type":"focus","focused":true}`,
		"pointer button":        `{"schemaVersion":1,"type":"pointer","action":"down","button":5,"buttons":1,"row":0,"column":0,"x":1,"y":2}`,
		"pointer buttons":       `{"schemaVersion":1,"type":"pointer","action":"down","button":0,"buttons":32,"row":0,"column":0,"x":1,"y":2}`,
		"wheel mode":            `{"schemaVersion":1,"type":"wheel","deltaX":1,"deltaY":2,"deltaMode":3,"row":0,"column":0}`,
		"resize dimension":      `{"schemaVersion":1,"type":"resize","columns":0,"rows":24,"pixelWidth":800,"pixelHeight":480,"cellWidth":10,"cellHeight":20,"devicePixelRatio":1}`,
		"non-finite coordinate": `{"schemaVersion":1,"type":"pointer","action":"move","button":-1,"buttons":0,"row":0,"column":0,"x":1e999,"y":2}`,
		"trailing value":        `{"schemaVersion":1,"type":"focus","focused":true} {}`,
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeEvent([]byte(data), EventLimits{})
			require.Error(t, err)
		})
	}
}

func FuzzDecodeEvent(f *testing.F) {
	f.Add([]byte(`{"schemaVersion":1,"type":"focus","focused":true}`))
	f.Add([]byte(`{"schemaVersion":1,"type":"text","text":"hello"}`))
	f.Add([]byte(`not json`))
	f.Add([]byte(`{"schemaVersion":1,"type":"text","text":"` + strings.Repeat("x", 4<<10) + `"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		event, err := DecodeEvent(data, EventLimits{MaxEventBytes: 4 << 10, MaxTextBytes: 512, MaxPasteBytes: 512})
		if len(data) > 4<<10 {
			require.ErrorIs(t, err, ErrEventLimit)
			return
		}
		if err != nil {
			return
		}
		payloads := map[EventKind]bool{
			EventText:    event.Text != nil,
			EventKey:     event.Key != nil,
			EventPaste:   event.Paste != nil,
			EventPointer: event.Pointer != nil,
			EventWheel:   event.Wheel != nil,
			EventResize:  event.Resize != nil,
			EventFocus:   event.Focus != nil,
		}
		set := 0
		for _, present := range payloads {
			if present {
				set++
			}
		}
		if set != 1 || !payloads[event.Kind] {
			t.Fatalf("decoded event violates the closed union: %#v", event)
		}
	})
}
