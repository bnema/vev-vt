package vt

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"sync"

	renderer "github.com/bnema/vev-vt/core"
)

// ErrHistoryCorrupt reports corruption of an internally compressed sealed page.
// Public history decoders reject malformed external data before installing it.
var ErrHistoryCorrupt = errors.New("corrupt compressed history page")

// sealedPage owns immutable semantic contents and mutable physical backing.
// Evicted-prefix wrappers and borrowed views share this owner. A returned Frame
// is an internal read-only borrow: dropping the cached pointer cannot invalidate
// an in-flight reader because Go keeps that reader's page reference alive.
type sealedPage struct {
	mu              sync.Mutex
	frame           renderer.Frame
	compressed      []byte
	encodedSize     int
	width, height   int
	reads, observed uint64
	observedOnce    bool
	incompressible  bool
	restores        uint64
}

func newSealedPage(frame renderer.Frame) *sealedPage {
	return &sealedPage{frame: frame, width: frame.Width, height: frame.Height}
}

func (p *sealedPage) readFrame(cache bool) (renderer.Frame, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reads++
	if p.frame.Height != 0 {
		return p.frame, nil
	}
	frame, err := p.decode()
	if err != nil {
		return renderer.Frame{}, err
	}
	p.restores++
	if cache {
		p.frame = frame
	}
	return frame, nil
}

// decode is called with the page lock held. The exact uncompressed length and
// zlib checksum are validated before VTC1's full semantic preflight runs.
func (p *sealedPage) decode() (renderer.Frame, error) {
	invalid := func(err error) (renderer.Frame, error) {
		return renderer.Frame{}, fmt.Errorf("%w: %v", ErrHistoryCorrupt, err)
	}
	if p.encodedSize < historyHeaderBytes || p.encodedSize > 256<<20 || len(p.compressed) == 0 {
		return invalid(errors.New("missing backing"))
	}
	source := bytes.NewReader(p.compressed)
	r, err := zlib.NewReader(source)
	if err != nil {
		return invalid(err)
	}
	data, readErr := io.ReadAll(io.LimitReader(r, int64(p.encodedSize)+1))
	closeErr := r.Close()
	if readErr != nil {
		return invalid(readErr)
	}
	if closeErr != nil {
		return invalid(closeErr)
	}
	if len(data) != p.encodedSize || source.Len() != 0 {
		return invalid(errors.New("backing length mismatch"))
	}
	view, err := UnmarshalHistory(data)
	if err != nil {
		return invalid(err)
	}
	if len(view.chunks) != 1 {
		return invalid(errors.New("unexpected page count"))
	}
	frame := view.chunks[0].page.frame
	if frame.Width != p.width || frame.Height != p.height {
		return invalid(errors.New("page geometry mismatch"))
	}
	return frame, nil
}

func (c *HistoryChunk) frameView() renderer.Frame {
	frame, err := c.page.readFrame(true)
	if err != nil {
		panic(err)
	} // Never substitute blank data for corrupted history.
	return frame
}

// Restore validates and caches the chunk's backing before a caller starts a
// read transaction. Ordinary reads restore transparently; if private backing is
// corrupted they panic with ErrHistoryCorrupt rather than silently losing text.
func (c *HistoryChunk) Restore() error {
	if c == nil {
		return nil
	}
	_, err := c.page.readFrame(true)
	return err
}

func (p *sealedPage) compressIfIdle() (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.observedOnce || p.observed != p.reads {
		p.observed, p.observedOnce = p.reads, true
		return false, nil
	}
	if p.frame.Height == 0 || p.incompressible {
		return false, nil
	}
	if len(p.compressed) != 0 {
		p.frame = renderer.Frame{}
		return true, nil
	}
	// Encode the full backing, not an evicted-prefix wrapper. Synthetic IDs are
	// local to this private envelope; public stable IDs remain on the wrappers.
	chunk := &HistoryChunk{page: newSealedPage(p.frame), count: p.height, width: p.width,
		bounds: make([]LineBound, p.height), rowIDs: make([]RowID, p.height)}
	for y := range p.height {
		chunk.bounds[y] = LineBound{End: p.width}
		chunk.rowIDs[y] = RowID(y + 1)
	}
	raw, err := MarshalHistory(HistoryView{chunks: []*HistoryChunk{chunk}, rows: p.height, nextRowID: RowID(p.height + 1)})
	if err != nil {
		return false, err
	}
	var out bytes.Buffer
	w, err := zlib.NewWriterLevel(&out, zlib.BestSpeed)
	if err != nil {
		return false, err
	}
	if _, err := w.Write(raw); err != nil {
		_ = w.Close()
		return false, err
	}
	if err := w.Close(); err != nil {
		return false, err
	}
	if out.Len() >= len(raw) {
		p.incompressible = true
		return false, nil
	}
	p.compressed = out.Bytes()
	p.encodedSize = len(raw)
	p.frame = renderer.Frame{}
	return true, nil
}

// CompressIdle visits at most maxPages sealed pages. Call it from the history
// owner's idle scheduler, never concurrently with append/eviction. A page must
// remain unread across two visits; the newest sealed chunk and mutable tail are
// kept hot. There are no internal goroutines, clocks, pooling or mmap mappings.
func (h *History) CompressIdle(maxPages int) (int, error) {
	if h == nil || maxPages <= 0 || len(h.chunks) < 2 {
		return 0, nil
	}
	eligible := len(h.chunks) - 1
	compressed := 0
	for range min(maxPages, eligible) {
		h.compressionCursor %= eligible
		chunk := h.chunks[h.compressionCursor]
		h.compressionCursor++
		didCompress, err := chunk.page.compressIfIdle()
		if err != nil {
			return compressed, err
		}
		if didCompress {
			compressed++
		}
	}
	return compressed, nil
}

// HistoryCompressionStats describes retained physical backing, not process RSS.
// ResidentLogicalBytes excludes Go allocator/map overhead; CompressedBytes is
// the encoded backing size. Retention remains governed by LogicalBytes alone.
type HistoryCompressionStats struct {
	ColdPages            int
	CompressedBytes      uint64
	ResidentLogicalBytes uint64
	Restores             uint64
}

func (h *History) CompressionStats() HistoryCompressionStats {
	var stats HistoryCompressionStats
	if h == nil {
		return stats
	}
	stats.ResidentLogicalBytes = h.tailBytes
	for _, chunk := range h.chunks {
		p := chunk.page
		p.mu.Lock()
		if p.frame.Height == 0 {
			stats.ColdPages++
		} else {
			stats.ResidentLogicalBytes += p.frame.LogicalBytes()
		}
		stats.CompressedBytes += uint64(len(p.compressed))
		stats.Restores += p.restores
		p.mu.Unlock()
	}
	return stats
}
