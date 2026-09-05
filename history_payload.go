package vt

import renderer "github.com/bnema/vev-vt/core"

func (c *HistoryChunk) recordPayloads() {
	frame := c.frameView()
	var last map[renderer.CellPayload]int
	for row := range c.count {
		for x := range c.width {
			p := frame.Cell(x, c.start+row).Payload
			if p.Empty() {
				continue
			}
			if last == nil {
				last = make(map[renderer.CellPayload]int)
			}
			last[p] = c.start + row
		}
	}
	c.payloadBytes = 0
	c.payloadDrops = nil
	if len(last) == 0 {
		return
	}
	c.payloadDrops = make([]uint64, frame.Height)
	for value, row := range last {
		bytes := value.LogicalBytes()
		c.payloadBytes += bytes
		c.payloadDrops[row] += bytes
	}
}

func (c *HistoryChunk) withoutFirstRow() *HistoryChunk {
	payloadBytes := c.payloadBytes
	if len(c.payloadDrops) != 0 {
		payloadBytes -= c.payloadDrops[c.start]
	}
	return &HistoryChunk{
		page: c.page, start: c.start + 1, count: c.count - 1, width: c.width,
		bounds: c.bounds[1:], rowIDs: c.rowIDs[1:],
		styleDrops: c.styleDrops, styleCount: c.styleCount - c.styleDrops[c.start],
		payloadDrops: c.payloadDrops, payloadBytes: payloadBytes,
	}
}

func (h *History) recordPayloadScratch(p renderer.CellPayload) {
	if p.Empty() {
		return
	}
	if h.payloadScratch == nil {
		h.payloadScratch = make(map[renderer.CellPayload]struct{})
	}
	h.payloadScratch[p] = struct{}{}
}

func (h *History) rowPayloadBytes() uint64 {
	var n uint64
	for p := range h.payloadScratch {
		n += p.LogicalBytes()
	}
	return n
}
