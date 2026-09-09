package html

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bnema/vev-vt/core"
	"github.com/stretchr/testify/require"
)

func TestDefaultThemeMatchesStylesheetFallbacks(t *testing.T) {
	stylesheet := strings.NewReplacer(" ", "", "\n", "", "\t", "").Replace(Stylesheet())
	for declaration := range strings.SplitSeq(DefaultTheme().CSS(), ";") {
		if declaration != "" {
			require.Contains(t, stylesheet, declaration)
		}
	}
}

func TestThemeCSSUsesOnlyTypedColorValues(t *testing.T) {
	theme := DefaultTheme()
	theme.Foreground = core.RGB{R: 1, G: 2, B: 3}
	css := theme.CSS()

	require.Contains(t, css, "--vev-fg:#010203")
	require.Contains(t, css, "--vev-color-15:")
	require.Equal(t, 21, strings.Count(css, "--vev-"))
	require.NotEmpty(t, Stylesheet())

	encoded, err := json.Marshal(theme)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"foreground":{"r":1,"g":2,"b":3}`)
	require.NotContains(t, string(encoded), `"Foreground"`)
}
