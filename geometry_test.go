package vt

import "testing"

func TestGeometryValidityAndPixelsKnown(t *testing.T) {
	tests := []struct {
		name            string
		geometry        Geometry
		valid, hasPixel bool
	}{
		{name: "cells only", geometry: Geometry{Cols: 80, Rows: 24}, valid: true},
		{name: "full", geometry: Geometry{Cols: 80, Rows: 24, PixelWidth: 640, PixelHeight: 384}, valid: true, hasPixel: true},
		{name: "partial pixels are unknown", geometry: Geometry{Cols: 80, Rows: 24, PixelWidth: 640}, valid: true},
		{name: "zero columns", geometry: Geometry{Rows: 24, PixelWidth: 640, PixelHeight: 384}, hasPixel: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.geometry.Valid(); got != tt.valid {
				t.Fatalf("Valid() = %t, want %t", got, tt.valid)
			}
			if got := tt.geometry.PixelsKnown(); got != tt.hasPixel {
				t.Fatalf("PixelsKnown() = %t, want %t", got, tt.hasPixel)
			}
		})
	}
}
