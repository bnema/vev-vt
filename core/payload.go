package core

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	MaxGraphemeBytes  = 128
	MaxHyperlinkBytes = 4096
	// PayloadRecordLogicalBytes accounts for two fixed-width string descriptors.
	PayloadRecordLogicalBytes uint64 = 16
)

var ErrInvalidCellPayload = errors.New("invalid cell payload")

// CellPayload is an immutable semantic exceptional value. Its zero value is
// empty. Storage support does not imply parsing support for additional terminal
// protocols; a payload never exposes a page-local handle.
type CellPayload struct {
	grapheme  string
	hyperlink string
}

// NewCellPayload validates bounded UTF-8 strings without terminal controls.
// It copies nonempty strings so a small value cannot retain an oversized input.
func NewCellPayload(grapheme, hyperlink string) (CellPayload, error) {
	if !validPayloadString(grapheme, MaxGraphemeBytes) || !validPayloadString(hyperlink, MaxHyperlinkBytes) {
		return CellPayload{}, ErrInvalidCellPayload
	}
	return CellPayload{grapheme: strings.Clone(grapheme), hyperlink: strings.Clone(hyperlink)}, nil
}

func validPayloadString(value string, limit int) bool {
	if len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r >= 0x7f && r <= 0x9f {
			return false
		}
	}
	return true
}

func (p CellPayload) Grapheme() string  { return p.grapheme }
func (p CellPayload) Hyperlink() string { return p.hyperlink }
func (p CellPayload) Empty() bool       { return p == CellPayload{} }
func (p CellPayload) LogicalBytes() uint64 {
	if p.Empty() {
		return 0
	}
	return PayloadRecordLogicalBytes + uint64(len(p.grapheme)) + uint64(len(p.hyperlink))
}

type payloadSlot struct {
	value CellPayload
	refs  uint32
}

// internPayload returns a page-local handle and acquires one reference. Slot
// zero is the implicit empty payload and requires no dictionary allocation.
func (f Frame) internPayload(value CellPayload) uint32 {
	if value.Empty() {
		return 0
	}
	p := f.page
	if id, ok := p.payloadIndex[value]; ok {
		p.payloads[id-1].refs++
		return id
	}
	if p.payloadIndex == nil {
		p.payloadIndex = make(map[CellPayload]uint32)
	}
	var id uint32
	if n := len(p.freePayloads); n > 0 {
		id = p.freePayloads[n-1]
		p.freePayloads = p.freePayloads[:n-1]
		p.payloads[id-1] = payloadSlot{value: value, refs: 1}
	} else {
		p.payloads = append(p.payloads, payloadSlot{value: value, refs: 1})
		id = uint32(len(p.payloads))
	}
	p.payloadIndex[value] = id
	p.payloadBytes += value.LogicalBytes()
	return id
}

func (f Frame) releasePayload(id uint32) {
	if id == 0 {
		return
	}
	p := f.page
	slot := &p.payloads[id-1]
	slot.refs--
	if slot.refs != 0 {
		return
	}
	delete(p.payloadIndex, slot.value)
	p.payloadBytes -= slot.value.LogicalBytes()
	*slot = payloadSlot{}
	p.freePayloads = append(p.freePayloads, id)
}

func (f Frame) payload(id uint32) CellPayload {
	if id == 0 {
		return CellPayload{}
	}
	return f.page.payloads[id-1].value
}

func (f Frame) checkPayloadInvariants() error {
	p := f.page
	refs := make([]uint32, len(p.payloads))
	for _, cell := range p.cells {
		id := cell.payloadID
		if id == 0 {
			continue
		}
		if uint64(id) > uint64(len(refs)) {
			return fmt.Errorf("unresolved payload ID %d", id)
		}
		refs[id-1]++
	}
	free := make([]bool, len(p.payloads))
	for _, id := range p.freePayloads {
		if id == 0 || uint64(id) > uint64(len(free)) || free[id-1] {
			return fmt.Errorf("invalid free payload ID %d", id)
		}
		free[id-1] = true
	}
	var total uint64
	used := 0
	for i, slot := range p.payloads {
		if slot.refs != refs[i] {
			return fmt.Errorf("payload %d reference mismatch", i+1)
		}
		if slot.refs == 0 {
			if !free[i] || !slot.value.Empty() {
				return fmt.Errorf("unreclaimed payload %d", i+1)
			}
			continue
		}
		if free[i] || slot.value.Empty() || p.payloadIndex[slot.value] != uint32(i+1) {
			return fmt.Errorf("invalid payload dictionary entry %d", i+1)
		}
		if !validPayloadString(slot.value.grapheme, MaxGraphemeBytes) || !validPayloadString(slot.value.hyperlink, MaxHyperlinkBytes) {
			return ErrInvalidCellPayload
		}
		total += slot.value.LogicalBytes()
		used++
	}
	if total != p.payloadBytes || used != len(p.payloadIndex) {
		return fmt.Errorf("payload accounting mismatch")
	}
	return nil
}
