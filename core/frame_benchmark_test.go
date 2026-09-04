package core

import "testing"

func BenchmarkCompactFrameBuild10Kx120(b *testing.B) {
	const rows, columns = 10_000, 120
	style := Style{Bold: true, Foreground: 4}
	b.ReportAllocs()
	b.ReportMetric(rows*columns, "cells/op")
	var logicalBytes uint64
	for b.Loop() {
		frame := NewFrame(columns, rows)
		for y := range rows {
			for x := range columns {
				frame.Set(x, y, Cell{Rune: 'x', Style: style})
			}
		}
		logicalBytes = frame.LogicalBytes()
	}
	b.ReportMetric(float64(logicalBytes), "logical-B/op")
}
