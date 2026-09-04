package core

import "testing"

func BenchmarkCompactFrameBuild10Kx120(b *testing.B) {
	const rows, columns = 10_000, 120
	style := Style{Bold: true, Foreground: 4}
	b.ReportAllocs()
	b.ReportMetric(rows*columns, "cells/op")
	for b.Loop() {
		frame := NewFrame(columns, rows)
		for y := range rows {
			for x := range columns {
				frame.Set(x, y, Cell{Rune: 'x', Style: style})
			}
		}
		b.ReportMetric(float64(frame.LogicalBytes()), "logical-B/op")
	}
}
