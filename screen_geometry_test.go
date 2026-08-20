package vt

import "testing"

func TestScreenWindowSizeReports(t *testing.T) {
	tests := []struct {
		name     string
		geometry Geometry
		query    string
		want     string
	}{
		{name: "window pixels", geometry: Geometry{Cols: 80, Rows: 24, PixelWidth: 640, PixelHeight: 384}, query: "\x1b[14t", want: "\x1b[4;384;640t"},
		{name: "cell pixels", geometry: Geometry{Cols: 80, Rows: 24, PixelWidth: 640, PixelHeight: 384}, query: "\x1b[16t", want: "\x1b[6;16;8t"},
		{name: "cell pixels truncate", geometry: Geometry{Cols: 80, Rows: 24, PixelWidth: 803, PixelHeight: 487}, query: "\x1b[16t", want: "\x1b[6;20;10t"},
		{name: "unknown pixels", geometry: Geometry{Cols: 80, Rows: 24}, query: "\x1b[14t"},
		{name: "partial pixels are unknown", geometry: Geometry{Cols: 80, Rows: 24, PixelWidth: 640}, query: "\x1b[16t"},
		{name: "unrelated window query", geometry: Geometry{Cols: 80, Rows: 24, PixelWidth: 640, PixelHeight: 384}, query: "\x1b[18t"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScreen(tt.geometry.Cols, tt.geometry.Rows)
			s.SetGeometry(tt.geometry)
			var got string
			s.OnResponse = func(response []byte) { got += string(response) }
			s.Write([]byte(tt.query))
			if got != tt.want {
				t.Fatalf("response = %q, want %q", got, tt.want)
			}
		})
	}
}
