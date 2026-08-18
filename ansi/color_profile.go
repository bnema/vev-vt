package ansi

// ColorProfile describes the color sequences an output target can represent.
// The zero value preserves the renderer's historical truecolor behavior.
type ColorProfile uint8

const (
	// ColorProfileTrueColor emits direct RGB colors and preserves indexed colors.
	ColorProfileTrueColor ColorProfile = iota
	// ColorProfileANSI256 maps RGB colors to the fixed xterm 256-color palette.
	ColorProfileANSI256
	// ColorProfileANSI16 emits basic and bright 16-color SGR sequences.
	ColorProfileANSI16
	// ColorProfileMonochrome omits explicit colors while preserving attributes.
	ColorProfileMonochrome
)

var ansi16Palette = [...]RGB{
	{R: 0x00, G: 0x00, B: 0x00},
	{R: 0x80, G: 0x00, B: 0x00},
	{R: 0x00, G: 0x80, B: 0x00},
	{R: 0x80, G: 0x80, B: 0x00},
	{R: 0x00, G: 0x00, B: 0x80},
	{R: 0x80, G: 0x00, B: 0x80},
	{R: 0x00, G: 0x80, B: 0x80},
	{R: 0xc0, G: 0xc0, B: 0xc0},
	{R: 0x80, G: 0x80, B: 0x80},
	{R: 0xff, G: 0x00, B: 0x00},
	{R: 0x00, G: 0xff, B: 0x00},
	{R: 0xff, G: 0xff, B: 0x00},
	{R: 0x00, G: 0x00, B: 0xff},
	{R: 0xff, G: 0x00, B: 0xff},
	{R: 0x00, G: 0xff, B: 0xff},
	{R: 0xff, G: 0xff, B: 0xff},
}

var ansi256CubeLevels = [...]uint8{0, 95, 135, 175, 215, 255}

func rgbToANSI16(rgb RGB) int {
	best, bestDistance := 0, colorDistance(rgb, ansi16Palette[0])
	for index := 1; index < len(ansi16Palette); index++ {
		if distance := colorDistance(rgb, ansi16Palette[index]); distance < bestDistance {
			best, bestDistance = index, distance
		}
	}
	return best
}

func rgbToANSI256(rgb RGB) int {
	ri, r := nearestCubeLevel(rgb.R)
	gi, g := nearestCubeLevel(rgb.G)
	bi, b := nearestCubeLevel(rgb.B)
	cubeIndex := 16 + 36*ri + 6*gi + bi
	cubeDistance := colorDistance(rgb, RGB{R: r, G: g, B: b})

	grayIndex, grayDistance := 232, colorDistance(rgb, RGB{R: 8, G: 8, B: 8})
	for offset := 1; offset < 24; offset++ {
		level := uint8(8 + 10*offset)
		if distance := colorDistance(rgb, RGB{R: level, G: level, B: level}); distance < grayDistance {
			grayIndex, grayDistance = 232+offset, distance
		}
	}
	if grayDistance < cubeDistance {
		return grayIndex
	}
	return cubeIndex
}

func nearestCubeLevel(value uint8) (int, uint8) {
	best, bestDistance := 0, absInt(int(value)-int(ansi256CubeLevels[0]))
	for index := 1; index < len(ansi256CubeLevels); index++ {
		if distance := absInt(int(value) - int(ansi256CubeLevels[index])); distance < bestDistance {
			best, bestDistance = index, distance
		}
	}
	return best, ansi256CubeLevels[best]
}

func ansi256ToRGB(index int) (RGB, bool) {
	if index < 0 || index > 255 {
		return RGB{}, false
	}
	if index < len(ansi16Palette) {
		return ansi16Palette[index], true
	}
	if index < 232 {
		index -= 16
		return RGB{
			R: ansi256CubeLevels[index/36],
			G: ansi256CubeLevels[(index/6)%6],
			B: ansi256CubeLevels[index%6],
		}, true
	}
	level := uint8(8 + (index-232)*10)
	return RGB{R: level, G: level, B: level}, true
}

func colorDistance(a, b RGB) int {
	dr := int(a.R) - int(b.R)
	dg := int(a.G) - int(b.G)
	db := int(a.B) - int(b.B)
	return dr*dr + dg*dg + db*db
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
