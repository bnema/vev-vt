package vt

import (
	"encoding/binary"

	renderer "github.com/bnema/vev-vt/core"
)

func appendHistoryPayload(dst []byte, value renderer.CellPayload) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(value.Grapheme())))
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(value.Hyperlink())))
	dst = append(dst, value.Grapheme()...)
	return append(dst, value.Hyperlink()...)
}

func (p *historyParser) payloadDictionary(count uint32, stats *historyDecodeStats) ([]renderer.CellPayload, bool) {
	if count == 0 {
		return nil, true
	}
	// The record budget bounds the table before allocating any string values.
	if uint64(count)*renderer.PayloadRecordLogicalBytes > maxHistoryPayloadBytes-stats.payloadBytes {
		return nil, false
	}
	values := make([]renderer.CellPayload, count)
	seen := make(map[renderer.CellPayload]struct{})
	for id := range count {
		grapheme, gok := p.uint32()
		link, lok := p.uint32()
		length := uint64(grapheme) + uint64(link)
		if !gok || !lok || grapheme > renderer.MaxGraphemeBytes || link > renderer.MaxHyperlinkBytes || length == 0 || length > uint64(len(p.data)) {
			return nil, false
		}
		bytes := length + renderer.PayloadRecordLogicalBytes
		if !addHistoryDecodeBudget(&stats.payloadBytes, bytes, maxHistoryPayloadBytes) || !addHistoryDecodeBudget(&stats.bytes, bytes, maxHistoryDecodedBytes) {
			return nil, false
		}
		value, err := renderer.NewCellPayload(string(p.data[:grapheme]), string(p.data[grapheme:length]))
		if err != nil {
			return nil, false
		}
		p.data = p.data[length:]
		if _, duplicate := seen[value]; duplicate {
			return nil, false
		}
		seen[value] = struct{}{}
		values[id] = value
	}
	return values, true
}
