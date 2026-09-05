package core

type RGB struct {
	R uint8
	G uint8
	B uint8
}

type StyleAttrs uint16

const (
	AttrDim StyleAttrs = 1 << iota
	AttrUnderline
	AttrBlink
	AttrStrikethrough
)

type UnderlineStyle uint8

const (
	UnderlineNone UnderlineStyle = iota
	UnderlineSingle
	UnderlineDouble
	UnderlineCurly
	UnderlineDotted
	UnderlineDashed
)

type Style struct {
	Bold       bool
	Italic     bool
	Inverse    bool
	Attrs      StyleAttrs
	Foreground int // -1 means unset; ignored when HasForegroundRGB is true
	Background int // -1 means unset; ignored when HasBackgroundRGB is true

	HasForegroundRGB bool
	ForegroundRGB    RGB
	HasBackgroundRGB bool
	BackgroundRGB    RGB

	UnderlineStyle       UnderlineStyle
	HasUnderlineColor    bool
	UnderlineColor       int // ignored when HasUnderlineColorRGB is true
	HasUnderlineColorRGB bool
	UnderlineColorRGB    RGB
}

func DefaultStyle() Style { return Style{Foreground: -1, Background: -1} }

// Canonical returns the unique representation of s under Equal. Fields that
// are inactive for the selected color form are cleared. Canonical does not
// treat the zero Style as the default style: indexed color zero remains
// distinct from the unset colors returned by DefaultStyle.
func (s Style) Canonical() Style {
	if s.HasForegroundRGB {
		s.Foreground = 0
	} else {
		s.ForegroundRGB = RGB{}
	}
	if s.HasBackgroundRGB {
		s.Background = 0
	} else {
		s.BackgroundRGB = RGB{}
	}
	if s.HasUnderlineColorRGB {
		s.HasUnderlineColor = false
		s.UnderlineColor = 0
	} else {
		s.UnderlineColorRGB = RGB{}
		if !s.HasUnderlineColor {
			s.UnderlineColor = 0
		}
	}
	return s
}

func (s Style) Equal(other Style) bool {
	if s.Bold != other.Bold || s.Italic != other.Italic || s.Inverse != other.Inverse || s.Attrs != other.Attrs || s.UnderlineStyle != other.UnderlineStyle {
		return false
	}
	if s.HasForegroundRGB != other.HasForegroundRGB || s.HasBackgroundRGB != other.HasBackgroundRGB || s.HasUnderlineColorRGB != other.HasUnderlineColorRGB {
		return false
	}
	if s.HasForegroundRGB {
		if s.ForegroundRGB != other.ForegroundRGB {
			return false
		}
	} else if s.Foreground != other.Foreground {
		return false
	}
	if s.HasBackgroundRGB {
		if s.BackgroundRGB != other.BackgroundRGB {
			return false
		}
	} else if s.Background != other.Background {
		return false
	}
	if s.HasUnderlineColorRGB {
		return s.UnderlineColorRGB == other.UnderlineColorRGB
	}
	if s.HasUnderlineColor != other.HasUnderlineColor {
		return false
	}
	if s.HasUnderlineColor {
		return s.UnderlineColor == other.UnderlineColor
	}
	return true
}
