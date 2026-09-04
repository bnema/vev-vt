package vt

import (
	"math"

	"github.com/bnema/vev-vt/core"
)

// MeasureHistoryBlob scans resource declarations without allocating. It is a
// sizing pass, NOT semantic validation: callers must still use
// PreflightHistoryBlob or UnmarshalHistory after accepting their aggregate
// budget. In particular, duplicate IDs/dictionary entries are not checked here.
func MeasureHistoryBlob(data []byte) (DecodeStats, error) {
	invalid := func() (DecodeStats, error) { return DecodeStats{}, errInvalidHistory }
	if len(data) < historyHeaderBytes || string(data[:4]) != historyMagic {
		return invalid()
	}
	p := historyParser{data: data[4:]}
	chunks, _ := p.uint32()
	next, _ := p.uint64()
	if chunks > maxHistoryChunks || uint64(chunks)*historyChunkHeaderBytes > uint64(len(p.data)) || next == 0 || next == math.MaxUint64 {
		return invalid()
	}
	stats := historyDecodeStats{chunks: uint64(chunks)}
	for range chunks {
		width, wok := p.uint32()
		rows, rok := p.uint32()
		styles, sok := p.uint32()
		payloads, pok := p.uint32()
		cells := uint64(width) * uint64(rows)
		if !wok || !rok || !sok || !pok || rows == 0 || rows > maxHistoryChunkRows || uint64(width) > uint64(math.MaxInt) || styles == 0 || styles > maxHistoryStylesPerChunk || uint64(styles) > cells+1 || uint64(payloads) > cells {
			return invalid()
		}
		if !addHistoryDecodeBudget(&stats.rows, uint64(rows), maxHistoryRows) || !addHistoryDecodeBudget(&stats.cells, cells, maxHistoryCells) || !addHistoryDecodeBudget(&stats.styles, uint64(styles), maxHistoryDecodeStyles) || !addHistoryLogicalBytes(&stats, cells, uint64(rows), uint64(styles)) {
			return invalid()
		}
		styleBytes := uint64(styles-1) * historyStyleBytes
		if styleBytes > uint64(len(p.data)) {
			return invalid()
		}
		p.data = p.data[styleBytes:]
		for range payloads {
			grapheme, gok := p.uint32()
			link, lok := p.uint32()
			length := uint64(grapheme) + uint64(link)
			if !gok || !lok || grapheme > core.MaxGraphemeBytes || link > core.MaxHyperlinkBytes || length == 0 || length > uint64(len(p.data)) {
				return invalid()
			}
			bytes := length + core.PayloadRecordLogicalBytes
			if !addHistoryDecodeBudget(&stats.payloadBytes, bytes, maxHistoryPayloadBytes) || !addHistoryDecodeBudget(&stats.bytes, bytes, maxHistoryDecodedBytes) {
				return invalid()
			}
			p.data = p.data[length:]
		}
		rowBytes := uint64(rows) * historyRowBytes
		if rowBytes > uint64(len(p.data)) {
			return invalid()
		}
		p.data = p.data[rowBytes:]
		for range cells {
			if _, _, _, _, ok := p.storedCell(styles, payloads); !ok {
				return invalid()
			}
		}
	}
	if len(p.data) != 0 {
		return invalid()
	}
	return DecodeStats{Chunks: stats.chunks, Rows: stats.rows, Cells: stats.cells, Styles: stats.styles, Bytes: stats.bytes}, nil
}
