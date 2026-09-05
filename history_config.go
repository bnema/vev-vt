package vt

import (
	"errors"
	"math"
)

var ErrInvalidHistoryConfig = errors.New("invalid history configuration")

// HistoryConfig enforces limits selected by the application. The library
// supplies no byte or line defaults. Each positive limit is an independent
// ceiling; zero omits that ceiling. Both zero disable history. Live grids are
// excluded. ChunkRows is an internal grouping hint in [1,256]; zero selects
// 256 and does not grant space beyond the application's retention limits.
type HistoryConfig struct {
	MaxRows   int
	MaxBytes  uint64
	ChunkRows int
}

func (c HistoryConfig) Validate() error {
	if c.MaxRows < 0 || c.ChunkRows < 0 || c.ChunkRows > maxHistoryChunkRows {
		return ErrInvalidHistoryConfig
	}
	return nil
}

// NewHistory constructs owned bounded history. Invalid programmer-supplied
// configuration panics; user-input boundaries should call Validate first.
func NewHistory(config HistoryConfig) *History {
	h := &History{nextRowID: 1}
	if err := h.SetLimits(config); err != nil {
		panic(err)
	}
	return h
}

// SetLimits validates first, then updates limits and evicts oldest rows before
// returning. Existing borrowed views remain valid. Disabling history releases
// its owned backing but preserves the next row identity. The owner must serialize
// this operation with all other History mutations.
func (h *History) SetLimits(config HistoryConfig) error {
	if h == nil {
		return ErrInvalidHistoryConfig
	}
	if err := config.Validate(); err != nil {
		return err
	}
	if config.MaxRows == 0 && config.MaxBytes == 0 {
		*h = History{nextRowID: h.NextRowID()}
		return nil
	}
	rows := config.MaxRows
	if rows == 0 {
		rows = math.MaxInt
	}
	chunkRows := config.ChunkRows
	if chunkRows == 0 {
		chunkRows = maxHistoryChunkRows
	}
	chunkRows = min(chunkRows, rows)
	bytes := config.MaxBytes
	if bytes == 0 {
		bytes = math.MaxUint64
	}
	if len(h.tail) >= chunkRows {
		h.sealTail()
	}
	h.maxRows, h.rowCeiling, h.maxBytes, h.chunkRows = rows, config.MaxRows, bytes, chunkRows
	h.byteCeiling = config.MaxBytes
	for h.rows > rows || h.logicalBytes > bytes {
		h.evictOldest()
	}
	clear(h.payloadScratch)
	return nil
}

// Limits reports the application's policy. A zero limit is omitted; both
// zero mean history is disabled. ChunkRows reports the resolved grouping size.
func (h *History) Limits() HistoryConfig {
	if h == nil {
		return HistoryConfig{}
	}
	return HistoryConfig{MaxRows: h.rowCeiling, MaxBytes: h.byteCeiling, ChunkRows: h.chunkRows}
}
