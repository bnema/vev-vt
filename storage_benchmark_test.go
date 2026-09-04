package vt

import (
	"runtime"
	"testing"

	"github.com/bnema/vev-vt/core"
)

const (
	storageBenchmarkRows  = 10_000
	storageBenchmarkWidth = 120
)

var benchmarkStorageHistorySink *History

type storageBenchmarkScenario struct {
	name string
	fill func([]core.Cell, int)
}

func BenchmarkHistoryBuild10Kx120(b *testing.B) {
	for _, scenario := range historyStorageScenarios() {
		b.Run(scenario.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkStorageHistorySink = buildStorageBenchmarkHistory(b, scenario.fill)
			}
			b.ReportMetric(storageBenchmarkRows*storageBenchmarkWidth, "cells/op")
		})
	}
}

// BenchmarkHistoryRetained10Kx120 isolates each sample with garbage collection
// and reports the live heap retained by one history. Use -benchtime=1x when
// comparing representations; elapsed time includes the measurement GCs.
func BenchmarkHistoryRetained10Kx120(b *testing.B) {
	for _, scenario := range historyStorageScenarios() {
		b.Run(scenario.name, func(b *testing.B) {
			var retained uint64
			for b.Loop() {
				benchmarkStorageHistorySink = nil
				runtime.GC()
				var before runtime.MemStats
				runtime.ReadMemStats(&before)

				benchmarkStorageHistorySink = buildStorageBenchmarkHistory(b, scenario.fill)
				runtime.GC()
				var after runtime.MemStats
				runtime.ReadMemStats(&after)
				if after.HeapAlloc >= before.HeapAlloc {
					retained += after.HeapAlloc - before.HeapAlloc
				}
			}
			benchmarkStorageHistorySink = nil
			if b.N > 0 {
				b.ReportMetric(float64(retained)/float64(b.N), "retained-B/op")
			}
		})
	}
}

func historyStorageScenarios() []storageBenchmarkScenario {
	defaultStyle := core.DefaultStyle()
	repeatedIndexed := core.Style{
		Bold:           true,
		Foreground:     196,
		Background:     17,
		UnderlineStyle: core.UnderlineSingle,
	}
	repeatedRGB := core.Style{
		Foreground:       -1,
		Background:       -1,
		HasForegroundRGB: true,
		ForegroundRGB:    core.RGB{R: 224, G: 108, B: 117},
		HasBackgroundRGB: true,
		BackgroundRGB:    core.RGB{R: 40, G: 44, B: 52},
	}
	return []storageBenchmarkScenario{
		{
			name: "plain-ascii",
			fill: func(row []core.Cell, y int) {
				for x := range row {
					row[x] = core.Cell{Rune: rune('a' + (x+y)%26), Style: defaultStyle}
				}
			},
		},
		{
			name: "repeated-indexed-style",
			fill: func(row []core.Cell, y int) {
				for x := range row {
					row[x] = core.Cell{Rune: rune('a' + (x+y)%26), Style: repeatedIndexed}
				}
			},
		},
		{
			name: "repeated-rgb-style",
			fill: func(row []core.Cell, y int) {
				for x := range row {
					row[x] = core.Cell{Rune: rune('a' + (x+y)%26), Style: repeatedRGB}
				}
			},
		},
		{
			name: "high-cardinality-rgb",
			fill: func(row []core.Cell, y int) {
				for x := range row {
					value := uint32(y*len(row) + x)
					style := core.Style{
						Foreground:       -1,
						Background:       -1,
						HasForegroundRGB: true,
						ForegroundRGB: core.RGB{
							R: uint8(value >> 16),
							G: uint8(value >> 8),
							B: uint8(value),
						},
					}
					row[x] = core.Cell{Rune: 'x', Style: style}
				}
			},
		},
		{
			name: "wide-unicode",
			fill: func(row []core.Cell, _ int) {
				for x := 0; x < len(row); x += 2 {
					row[x] = core.Cell{Rune: '界', Style: defaultStyle}
					row[x+1] = core.Cell{Style: defaultStyle, Continuation: true}
				}
			},
		},
		{
			name: "styled-blanks",
			fill: func(row []core.Cell, y int) {
				style := core.Style{Foreground: -1, Background: (y % 16) + 232}
				for x := range row {
					row[x] = core.Cell{Rune: ' ', Style: style}
				}
			},
		},
	}
}

func buildStorageBenchmarkHistory(b *testing.B, fill func([]core.Cell, int)) *History {
	b.Helper()
	history := NewHistory(HistoryConfig{
		MaxRows:   storageBenchmarkRows,
		MaxBytes:  1 << 30,
		ChunkRows: 256,
	})
	row := make([]core.Cell, storageBenchmarkWidth)
	for y := range storageBenchmarkRows {
		fill(row, y)
		if err := history.Append(row, LineBound{End: len(row)}); err != nil {
			b.Fatal(err)
		}
	}
	return history
}
