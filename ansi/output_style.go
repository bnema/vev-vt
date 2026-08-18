package ansi

type outputColorKind uint8

const (
	outputColorDefault outputColorKind = iota
	outputColorANSI16
	outputColorANSI256
	outputColorRGB
)

type outputColor struct {
	kind  outputColorKind
	index int
	rgb   RGB
}

type outputStyle struct {
	bold           bool
	italic         bool
	inverse        bool
	attrs          StyleAttrs
	underlineStyle UnderlineStyle
	foreground     outputColor
	background     outputColor
	underlineColor outputColor
}

type styleProjector struct {
	profile ColorProfile
}

func (p styleProjector) project(style Style) outputStyle {
	return outputStyle{
		bold:           style.Bold,
		italic:         style.Italic,
		inverse:        style.Inverse,
		attrs:          style.Attrs,
		underlineStyle: style.UnderlineStyle,
		foreground:     p.projectColor(style.Foreground, style.Foreground >= 0, style.ForegroundRGB, style.HasForegroundRGB, true),
		background:     p.projectColor(style.Background, style.Background >= 0, style.BackgroundRGB, style.HasBackgroundRGB, true),
		underlineColor: p.projectColor(style.UnderlineColor, style.HasUnderlineColor, style.UnderlineColorRGB, style.HasUnderlineColorRGB, false),
	}
}

func (p styleProjector) projectColor(index int, hasIndex bool, rgb RGB, hasRGB, basicColor bool) outputColor {
	switch p.profile {
	case ColorProfileANSI256:
		if hasRGB {
			return outputColor{kind: outputColorANSI256, index: rgbToANSI256(rgb)}
		}
		if hasIndex && index >= 0 && index <= 255 {
			return outputColor{kind: outputColorANSI256, index: index}
		}
	case ColorProfileANSI16:
		if !basicColor {
			return outputColor{}
		}
		if hasRGB {
			return outputColor{kind: outputColorANSI16, index: rgbToANSI16(rgb)}
		}
		if hasIndex {
			if index >= 0 && index < 16 {
				return outputColor{kind: outputColorANSI16, index: index}
			}
			if resolved, ok := ansi256ToRGB(index); ok {
				return outputColor{kind: outputColorANSI16, index: rgbToANSI16(resolved)}
			}
		}
	case ColorProfileMonochrome:
		return outputColor{}
	default:
		if hasRGB {
			return outputColor{kind: outputColorRGB, rgb: rgb}
		}
		if hasIndex {
			return outputColor{kind: outputColorANSI256, index: index}
		}
	}
	return outputColor{}
}
