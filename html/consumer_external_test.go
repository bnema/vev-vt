package html_test

import (
	"encoding/json"
	"testing"

	"github.com/bnema/vev-vt/core"
	htmlrenderer "github.com/bnema/vev-vt/html"
)

func TestExternalHTMLConsumerUsesOnlyCore(t *testing.T) {
	frame := core.NewFrame(2, 1)
	frame.Set(0, 0, core.Cell{Rune: 'O', Style: core.DefaultStyle()})
	frame.Set(1, 0, core.Cell{Rune: 'K', Style: core.DefaultStyle()})

	renderer, err := htmlrenderer.New(htmlrenderer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := renderer.Prepare(frame, nil, false, htmlrenderer.Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(prepared.JSON()) {
		t.Fatalf("prepared JSON is invalid: %q", prepared.JSON())
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
}
