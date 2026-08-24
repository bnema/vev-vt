package main

import (
	"image"
	"image/color"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScaleFrameUsesNearestNeighbor(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 1, 1))
	source.SetRGBA(0, 0, color.RGBA{22, 62, 143, 255})

	scaled, err := scaleFrame(source, 2)
	require.NoError(t, err)
	require.Equal(t, image.Rect(0, 0, 2, 2), scaled.Bounds())
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			require.Equal(t, color.RGBA{22, 62, 143, 255}, scaled.RGBAAt(x, y))
		}
	}
}

func TestScaleFrameRejectsInvalidScale(t *testing.T) {
	_, err := scaleFrame(image.NewRGBA(image.Rect(0, 0, 1, 1)), 0)
	require.Error(t, err)
}

func TestScaleFrameRejectsMaximumIntScale(t *testing.T) {
	_, err := scaleFrame(image.NewRGBA(image.Rect(0, 0, 1, 1)), int(^uint(0)>>1))
	require.EqualError(t, err, "scaled frame exceeds 67108864 pixels")
}
