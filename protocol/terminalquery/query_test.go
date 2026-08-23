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

func TestProbeRequiresExactCapabilityResponses(t *testing.T) {
	for _, data := range []string{
		"\x1b_Gi=31;OK,extra\x1b\\",
		"\x1b_Gi=310;OK\x1b\\",
		"\x1b_Gi=31;OKAY\x1b\\",
		"\x1b[?1;;2c",
		"\x1b[?x;2c",
	} {
		var p Probe
		got := append(p.Feed([]byte(data)), p.Finish()...)
		require.False(t, p.Ready(), "response %q was accepted", data)
		require.Equal(t, data, string(got))
	}
}

func FuzzProbeNeverPanics(f *testing.F) {
	f.Add([]byte("text\x1b[?1;2c\x1b_Gi=31;OK\x1b\\"))
	f.Add([]byte("\x1b_G"))
	f.Fuzz(func(t *testing.T, data []byte) {
		var p Probe
		_ = p.Feed(data)
		_ = p.Finish()
	})
}

func TestProbeDoesNotRetainLargeInputFragments(t *testing.T) {
	var p Probe
	input := append(make([]byte, 1<<20), []byte("\x1b_G")...)
	got := append(p.Feed(input), p.Finish()...)
	require.Equal(t, input, got)
	require.LessOrEqual(t, cap(p.pending), maxProbeResponseBytes+2)
}

func TestProbeBoundsUnterminatedResponse(t *testing.T) {
	var p Probe
	input := append([]byte("prefix"), []byte("\x1b_G")...)
	input = append(input, make([]byte, maxProbeResponseBytes+1)...)
	got := append(p.Feed(input), p.Finish()...)
	require.False(t, p.Ready())
	require.Equal(t, input, got)
}
