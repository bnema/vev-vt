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
	next := *c
	next.dropFirstRow()
	return &next
}

// dropFirstRow advances the wrapper past its first row in place. Only a wrapper
// no view has captured may be mutated; see History.privateHead.
func (c *HistoryChunk) dropFirstRow() {
	if len(c.payloadDrops) != 0 {
		c.payloadBytes -= c.payloadDrops[c.start]
	}
	c.styleCount -= c.styleDrops[c.start]
	c.start++
	c.count--
	c.bounds = c.bounds[1:]
	c.rowIDs = c.rowIDs[1:]
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
