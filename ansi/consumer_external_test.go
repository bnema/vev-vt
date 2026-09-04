package ansi_test

import (
	"bytes"
	"testing"

	vt "github.com/bnema/vev-vt"
	"github.com/bnema/vev-vt/ansi"
	"github.com/bnema/vev-vt/core"
)

func TestExternalANSIConsumesScreenCellSource(t *testing.T) {
	screen := vt.NewScreen(2, 1)
	screen.Write([]byte("OK"))

	output, err := ansi.New(ansi.Capabilities{}).Draw(screen, []core.Damage{core.FullRedraw()})
	if err != nil {
		t.Fatalf("Draw: %v", err)
	}
	if !bytes.Contains(output, []byte("OK")) {
		t.Fatalf("ANSI output = %q, want visible screen content", output)
	}
}

func TestExternalANSIConsumesCoreFrameIndependently(t *testing.T) {
	frame := core.NewFrame(2, 1)
	frame.Set(0, 0, core.Cell{Rune: 'O', Style: core.DefaultStyle()})
	frame.Set(1, 0, core.Cell{Rune: 'K', Style: core.DefaultStyle()})

	output, err := ansi.New(ansi.Capabilities{}).Draw(frame, []core.Damage{core.FullRedraw()})
	if err != nil {
		t.Fatalf("Draw: %v", err)
	}
	if !bytes.Contains(output, []byte("OK")) {
		t.Fatalf("ANSI output = %q, want visible core frame content", output)
	}
}
