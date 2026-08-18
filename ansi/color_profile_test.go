package ansi

import "testing"

func TestRendererANSI256ProjectsRGBColors(t *testing.T) {
	frame := NewFrame(1, 1)
	frame.Set(0, 0, Cell{Rune: 'X', Style: Style{
		Foreground:           -1,
		Background:           -1,
		HasForegroundRGB:     true,
		ForegroundRGB:        RGB{R: 255, G: 0, B: 0},
		HasBackgroundRGB:     true,
		BackgroundRGB:        RGB{R: 8, G: 8, B: 8},
		HasUnderlineColorRGB: true,
		UnderlineColorRGB:    RGB{R: 128, G: 128, B: 128},
	}})

	out, err := NewWithColorProfile(Capabilities{}, ColorProfileANSI256).Draw(frame, []Damage{FullRedraw()})
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b[1;1H\x1b[0;38;5;196;48;5;232;58;5;244mX\x1b[0m"
	if got := string(out); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestRendererANSI16UsesBasicColorsAndOmitsUnderlineColor(t *testing.T) {
	frame := NewFrame(1, 1)
	frame.Set(0, 0, Cell{Rune: 'X', Style: Style{
		Bold:                 true,
		Attrs:                AttrUnderline,
		Foreground:           -1,
		Background:           4,
		HasForegroundRGB:     true,
		ForegroundRGB:        RGB{R: 255, G: 0, B: 0},
		UnderlineStyle:       UnderlineCurly,
		HasUnderlineColor:    true,
		UnderlineColor:       9,
		HasUnderlineColorRGB: true,
		UnderlineColorRGB:    RGB{R: 0, G: 255, B: 0},
	}})

	out, err := NewWithColorProfile(Capabilities{}, ColorProfileANSI16).Draw(frame, []Damage{FullRedraw()})
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b[1;1H\x1b[0;1;4:3;91;44mX\x1b[0m"
	if got := string(out); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestRendererANSI16DownsamplesExtendedIndexedColors(t *testing.T) {
	frame := NewFrame(1, 1)
	frame.Set(0, 0, Cell{Rune: 'X', Style: Style{Foreground: 196, Background: 21}})

	out, err := NewWithColorProfile(Capabilities{}, ColorProfileANSI16).Draw(frame, []Damage{FullRedraw()})
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b[1;1H\x1b[0;91;104mX\x1b[0m"
	if got := string(out); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestRendererIndexedColorsFollowProfile(t *testing.T) {
	style := Style{
		Foreground:        9,
		Background:        12,
		HasUnderlineColor: true,
		UnderlineColor:    10,
	}
	tests := []struct {
		name      string
		profile   ColorProfile
		wantStyle string
	}{
		{name: "truecolor preserves indexed colors", profile: ColorProfileTrueColor, wantStyle: "\x1b[0;38;5;9;48;5;12;58;5;10m"},
		{name: "ANSI-256 preserves indexed colors", profile: ColorProfileANSI256, wantStyle: "\x1b[0;38;5;9;48;5;12;58;5;10m"},
		{name: "ANSI-16 preserves basic indexes", profile: ColorProfileANSI16, wantStyle: "\x1b[0;91;104m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := NewFrame(1, 1)
			frame.Set(0, 0, Cell{Rune: 'X', Style: style})
			out, err := NewWithColorProfile(Capabilities{}, tt.profile).Draw(frame, []Damage{FullRedraw()})
			if err != nil {
				t.Fatal(err)
			}
			want := "\x1b[1;1H" + tt.wantStyle + "X\x1b[0m"
			if got := string(out); got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestRendererMonochromeOmitsColorsAndPreservesAttributes(t *testing.T) {
	frame := NewFrame(2, 1)
	style := Style{
		Bold:                 true,
		Italic:               true,
		Inverse:              true,
		Attrs:                AttrUnderline | AttrStrikethrough,
		Foreground:           -1,
		Background:           200,
		HasForegroundRGB:     true,
		ForegroundRGB:        RGB{R: 12, G: 34, B: 56},
		UnderlineStyle:       UnderlineDouble,
		HasUnderlineColor:    true,
		UnderlineColor:       9,
		HasUnderlineColorRGB: true,
		UnderlineColorRGB:    RGB{R: 200, G: 100, B: 50},
	}
	frame.Set(0, 0, Cell{Rune: 'A', Style: style})
	style.ForegroundRGB = RGB{R: 255, G: 0, B: 0}
	frame.Set(1, 0, Cell{Rune: 'B', Style: style})

	out, err := NewWithColorProfile(Capabilities{}, ColorProfileMonochrome).Draw(frame, []Damage{FullRedraw()})
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b[1;1H\x1b[0;1;3;21;7;9mAB\x1b[0m"
	if got := string(out); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestRendererColorProfilesApplyToFullIncrementalAndResetDraws(t *testing.T) {
	style := Style{
		Attrs:                AttrUnderline,
		Foreground:           -1,
		Background:           -1,
		HasForegroundRGB:     true,
		ForegroundRGB:        RGB{R: 255, G: 0, B: 0},
		HasBackgroundRGB:     true,
		BackgroundRGB:        RGB{R: 0, G: 0, B: 255},
		HasUnderlineColorRGB: true,
		UnderlineColorRGB:    RGB{R: 0, G: 255, B: 0},
	}
	tests := []struct {
		name    string
		profile ColorProfile
		sgr     string
	}{
		{name: "truecolor", profile: ColorProfileTrueColor, sgr: "\x1b[0;4;38;2;255;0;0;48;2;0;0;255;58;2;0;255;0m"},
		{name: "ANSI-256", profile: ColorProfileANSI256, sgr: "\x1b[0;4;38;5;196;48;5;21;58;5;46m"},
		{name: "ANSI-16", profile: ColorProfileANSI16, sgr: "\x1b[0;4;91;104m"},
		{name: "monochrome", profile: ColorProfileMonochrome, sgr: "\x1b[0;4m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renderer := NewWithColorProfile(Capabilities{}, tt.profile)
			frame := NewFrame(1, 1)
			frame.Set(0, 0, Cell{Rune: 'A', Style: style})
			wantFull := "\x1b[1;1H" + tt.sgr + "A\x1b[0m"
			out, err := renderer.Draw(frame, []Damage{FullRedraw()})
			if err != nil {
				t.Fatal(err)
			}
			if got := string(out); got != wantFull {
				t.Fatalf("full output = %q, want %q", got, wantFull)
			}

			frame.Set(0, 0, Cell{Rune: 'B', Style: style})
			wantIncremental := "\x1b[1;1H" + tt.sgr + "B\x1b[0m"
			out, err = renderer.Draw(frame, []Damage{{Kind: DamageText, X: 0, Y: 0, Width: 1, Height: 1}})
			if err != nil {
				t.Fatal(err)
			}
			if got := string(out); got != wantIncremental {
				t.Fatalf("incremental output = %q, want %q", got, wantIncremental)
			}

			unchanged, err := renderer.Draw(frame, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(unchanged) != 0 {
				t.Fatalf("unchanged draw = %q, want no output", unchanged)
			}

			renderer.Reset()
			out, err = renderer.Draw(frame, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(out); got != wantIncremental {
				t.Fatalf("post-reset output = %q, want %q", got, wantIncremental)
			}
		})
	}
}

func TestNewPreservesUnkeyedCapabilitiesAndTrueColorDefault(t *testing.T) {
	renderer := New(Capabilities{true})
	frame := NewFrame(1, 1)
	frame.Set(0, 0, Cell{Rune: 'X', Style: Style{
		Foreground:       -1,
		Background:       -1,
		HasForegroundRGB: true,
		ForegroundRGB:    RGB{R: 1, G: 2, B: 3},
	}})
	out, err := renderer.Draw(frame, []Damage{FullRedraw()})
	if err != nil {
		t.Fatal(err)
	}
	want := SyncStartCSI + "\x1b[1;1H\x1b[0;38;2;1;2;3mX\x1b[0m" + SyncEndCSI
	if got := string(out); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestConstrainedProfilesOmitInvalidIndexedColors(t *testing.T) {
	style := Style{
		Foreground:        256,
		Background:        257,
		HasUnderlineColor: true,
		UnderlineColor:    -1,
	}
	profiles := []struct {
		name    string
		profile ColorProfile
	}{
		{name: "ANSI-256", profile: ColorProfileANSI256},
		{name: "ANSI-16", profile: ColorProfileANSI16},
		{name: "monochrome", profile: ColorProfileMonochrome},
	}
	for _, tt := range profiles {
		t.Run(tt.name, func(t *testing.T) {
			frame := NewFrame(1, 1)
			frame.Set(0, 0, Cell{Rune: 'X', Style: style})
			out, err := NewWithColorProfile(Capabilities{}, tt.profile).Draw(frame, []Damage{FullRedraw()})
			if err != nil {
				t.Fatal(err)
			}
			want := "\x1b[1;1HX\x1b[0m"
			if got := string(out); got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestColorQuantizationContract(t *testing.T) {
	tests := []struct {
		name    string
		rgb     RGB
		ansi16  int
		ansi256 int
	}{
		{name: "black", rgb: RGB{}, ansi16: 0, ansi256: 16},
		{name: "bright red", rgb: RGB{R: 255}, ansi16: 9, ansi256: 196},
		{name: "exact cube color", rgb: RGB{R: 95, G: 135, B: 175}, ansi16: 8, ansi256: 67},
		{name: "first gray", rgb: RGB{R: 8, G: 8, B: 8}, ansi16: 0, ansi256: 232},
		{name: "middle gray", rgb: RGB{R: 128, G: 128, B: 128}, ansi16: 8, ansi256: 244},
		{name: "last gray", rgb: RGB{R: 238, G: 238, B: 238}, ansi16: 15, ansi256: 255},
		{name: "white", rgb: RGB{R: 255, G: 255, B: 255}, ansi16: 15, ansi256: 231},
		{name: "arbitrary RGB", rgb: RGB{R: 12, G: 34, B: 56}, ansi16: 0, ansi256: 235},
		{name: "cube midpoint chooses lower index", rgb: RGB{R: 115}, ansi16: 1, ansi256: 52},
		{name: "ANSI-16 tie chooses lower index", rgb: RGB{R: 64}, ansi16: 0, ansi256: 52},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rgbToANSI16(tt.rgb); got != tt.ansi16 {
				t.Errorf("rgbToANSI16(%+v) = %d, want %d", tt.rgb, got, tt.ansi16)
			}
			if got := rgbToANSI256(tt.rgb); got != tt.ansi256 {
				t.Errorf("rgbToANSI256(%+v) = %d, want %d", tt.rgb, got, tt.ansi256)
			}
		})
	}
}
