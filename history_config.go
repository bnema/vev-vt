package vt

import (
	"errors"
	"math"
)

const DefaultHistoryBytes uint64 = 50_000_000
const DefaultHistoryRows = 10_000

var ErrInvalidHistoryConfig = errors.New("invalid history configuration")

// HistoryConfig controls retained scrollback for one Screen (normally one
// pane), not a session-wide pool. Live primary/alternate grids are excluded.
// MaxRows is optional: zero with a positive MaxBytes enables byte-only retention.
// The entirely zero limits disable history. Positive MaxRows with zero MaxBytes
// uses DefaultHistoryBytes, independently of terminal width. ChunkRows is a
// grouping hint in [1,256]; zero selects 256, without a retention allowance.
type HistoryConfig struct {
	MaxRows   int
	MaxBytes  uint64
	ChunkRows int
}

func DefaultHistoryConfig() HistoryConfig {
	return HistoryConfig{MaxRows: DefaultHistoryRows, MaxBytes: DefaultHistoryBytes, ChunkRows: maxHistoryChunkRows}
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
		bytes = DefaultHistoryBytes
	}
	if len(h.tail) >= chunkRows {
		h.sealTail()
	}
	h.maxRows, h.rowCeiling, h.maxBytes, h.chunkRows = rows, config.MaxRows, bytes, chunkRows
	for h.rows > rows || h.logicalBytes > bytes {
		h.evictOldest()
	}
	clear(h.payloadScratch)
	return nil
}

// Limits reports the effective policy, including the resolved byte default.
// A zero MaxRows is an optional row ceiling, not an unlimited byte budget.
func (h *History) Limits() HistoryConfig {
	if h == nil {
		return HistoryConfig{}
	}
	return HistoryConfig{MaxRows: h.rowCeiling, MaxBytes: h.maxBytes, ChunkRows: h.chunkRows}
}
