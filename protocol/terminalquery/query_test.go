package terminalquery

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProbeRequiresKittyAndDA1(t *testing.T) {
	for _, tc := range []struct {
		name      string
		data      string
		kitty     bool
		da1       bool
		unrelated string
	}{
		{name: "both and input", data: "x\x1b[?1;2c" + "\x1b_Gi=31;OK\x1b\\y", kitty: true, da1: true, unrelated: "xy"},
		{name: "secondary DA is insufficient", data: "\x1b[>1;2c\x1b_Gi=31;OK\x1b\\", kitty: true, unrelated: "\x1b[>1;2c"},
		{name: "ordinary input", data: "hello", unrelated: "hello"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var p Probe
			got := append(p.Feed([]byte(tc.data)), p.Finish()...)
			require.Equal(t, tc.kitty, p.KittyGraphics())
			require.Equal(t, tc.da1, p.DA1())
			require.Equal(t, tc.unrelated, string(got))
		})
	}
}

func TestProbeRecognizesFragmentedResponsesAndReplaysInput(t *testing.T) {
	var p Probe
	var unrelated []byte
	for _, fragment := range []string{"a\x1b", "[?1;2", "c", "b\x1b_Gi=31;", "OK\x1b", "\\z"} {
		unrelated = append(unrelated, p.Feed([]byte(fragment))...)
	}
	unrelated = append(unrelated, p.Finish()...)
	require.True(t, p.Ready())
	require.Equal(t, "abz", string(unrelated))
}

func TestProbeBoundsUnterminatedResponse(t *testing.T) {
	var p Probe
	input := append([]byte("prefix"), []byte("\x1b_G")...)
	input = append(input, make([]byte, maxProbeResponseBytes+1)...)
	got := append(p.Feed(input), p.Finish()...)
	require.False(t, p.Ready())
	require.Equal(t, input, got)
}
