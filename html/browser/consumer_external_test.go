package browser_test

import (
	"testing"

	"github.com/bnema/vev-vt/html/browser"
)

func TestExternalBrowserConsumerUsesEmbeddedRuntimeAndNeutralEvents(t *testing.T) {
	if browser.JavaScript() == "" {
		t.Fatal("embedded browser runtime is empty")
	}
	event, err := browser.DecodeEvent([]byte(`{"schemaVersion":1,"type":"focus","focused":true}`), browser.EventLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if event.Focus == nil || !event.Focus.Focused {
		t.Fatalf("decoded event = %#v", event)
	}
}
