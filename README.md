# vev-vt

`github.com/bnema/vev-vt` is a frontend-neutral VT terminal engine for Go.
It provides the terminal screen, scrollback history, immutable snapshots,
stable history chunks, compact VTC1 history bytes, and the reusable cell/frame model.
The `ansi` package turns core frames and damage into transactional ANSI output.

## Packages

- The module root (`github.com/bnema/vev-vt`) owns VT parsing, screen state,
  history, snapshots, callbacks, and the public model aliases (`Cell`, `Style`,
  `RGB`, `Frame`, `Damage`, and `RuneWidth`).
- `github.com/bnema/vev-vt/core` owns the frontend-neutral cell, style, frame,
  damage, and width implementation. It has no terminal, renderer, transport,
  or application dependencies.
- `github.com/bnema/vev-vt/ansi` is the concrete ANSI output package. It consumes
  read-only core cell sources and damage; it does not define a renderer-backend
  interface.
- `github.com/bnema/vev-vt/graphics` owns bounded renderer-neutral raster assets,
  sparse placements, clipping fragments, and immutable scene snapshots. It has no
  Kitty, ANSI, VT-policy, or transport dependency.
- `github.com/bnema/vev-vt/protocol/kittygraphics` parses bounded Kitty graphics
  APC commands and translates the supported static/direct subset into `graphics`.

## Ownership contract

Stateful values are single-owner and are not internally synchronized. Serialize
screen writes, resizes, snapshots, and history mutations in one owner. Parsing
and callbacks are synchronous: callbacks run before `Write` returns and follow
parser event order. Consume or copy response bytes during the callback.

`Screen`, `ScreenSnapshot`, and `Frame` provide storage-independent semantic
cell reads through `core.CellSource`. Their row extraction methods return owned
copies. `HistoryView.Row` and `HistoryView.Range` decode caller-owned semantic
rows from compact sealed slabs. `HistoryView.Cell`, `RowWidth`, and `CopyRow`
support allocation-free navigation and caller-owned scratch buffers. Sealed
`HistoryChunk` identity is stable for the lifetime of a view. Live primary and
alternate grids, screen snapshots, recovery transcript captures, and copied
history snapshot tails all retain compact cells rather than semantic row arrays.
Repeated `HistorySnapshotView.Tail()` calls reuse the same immutable capture.

## Alpha stability

`DefaultStyle()` is the canonical unset style and differs from `Style{}`, whose
zero-valued color indexes are active. `Style.Canonical()` clears inactive color
payloads and produces a representation whose Go equality exactly matches
`Style.Equal`; it does not collapse `Style{}` into `DefaultStyle()`.

The module has no production dependencies. `github.com/stretchr/testify` is
used only by the test suite. During v0.x alpha development, public APIs and byte
formats may change without compatibility adapters or migration readers. Prefer
a direct, documented break and removal of obsolete code when that produces a
cleaner design. VEVS remains an application-owned outer snapshot envelope and
is intentionally not implemented here.

## History persistence

`MarshalHistory` and `UnmarshalHistory` use **VTC1**, replacing VTH3 directly.
Older formats are rejected; no migration reader or dual-format path is retained.
The format stores a next-row-ID counter, equal-width chunks, canonical local
style and exceptional-payload dictionaries, row IDs and bounds. Ordinary cells
use 9-byte rune/style-reference/flag records; exceptional cells add a 4-byte
payload reference. Default style and empty payload IDs are implicit. IDs are
rebuilt from semantic values, not copied from internal pages.

Decoding validates the entire payload before allocating compact frames. It rejects
invalid references, duplicate or unused styles, noncanonical style fields,
duplicate row IDs, invalid counters, truncation and trailing bytes. Aggregate
ceilings are 12,000 rows and 1,920,000 cells; wide rows are allowed within those
ceilings. Exceptional payload records and strings have a separate aggregate
64 MiB decode ceiling. Preflight `DecodeStats.Bytes` reports uncompressed logical
storage, not encoded length or allocator usage.

Measure encoded size, throughput and allocations with:

```sh
go test . -run '^$' -bench '^BenchmarkCompactHistoryCodec$' -benchmem
```

## Exceptional payload storage

`core.NewCellPayload(grapheme, hyperlink)` constructs an immutable value for
`Cell.Payload`. Values contain at most 128 grapheme bytes and 4,096 hyperlink
bytes; invalid UTF-8 and terminal control characters are rejected. Empty payload
ID zero costs no dictionary entry. Pages intern nonempty values locally, reclaim
unreferenced slots after overwrite/clear, and remap values on cross-page copies.
Clone, reflow, snapshots, history eviction and VTC1 persistence preserve payloads.

This is the storage foundation for exceptional values, not a new grapheme
segmentation engine or OSC 8 parser. The VT parser and ANSI output still support
their existing rune-based text semantics; callers must not infer new protocol
support from the ability to retain payload values.

## Scrollback limits

Use `DefaultHistoryConfig()` for **50,000,000 uncompressed logical bytes and
10,000 lines per screen/pane**. These are independent ceilings, not a session
pool. Primary/alternate live grids are excluded, and there is no extra page
allowance. `MaxRows: 0` with a positive `MaxBytes` means byte-only retention;
both limits zero disable history. A positive row limit with zero bytes selects
the fixed 50 MB byte default, not a width-derived cell budget.

`HistoryConfig.Validate()` rejects negative limits/grouping and grouping above
256 rows. Constructors panic for invalid programmer input; applications should
validate user configuration before construction. `History.SetLimits()` returns
validation errors without mutation and applies valid reductions immediately by
evicting oldest rows. Existing borrowed views remain valid. `Limits`, `Cap`,
`ByteCap`, `Len` and `LogicalBytes` report effective policy and usage.

Persistence has separate per-blob resource ceilings described above; increasing
retention does not bypass decoder budgets. Chunk-by-chunk snapshot persistence
avoids requiring one enormous history blob.

## Cold history compression

The owner may call `History.CompressIdle(maxPages)` from an idle timer. Each call
visits at most that many sealed pages. A page must remain unread across two
visits; the newest sealed page and mutable tail stay hot. Compression uses the
standard library's zlib implementation over validated VTC1 data, with no worker
goroutines, pooling, mmap or platform-specific memory operations.

Borrowed chunk identity and uncompressed retention accounting do not change.
Random-access reads restore a page into a cache; later idle visits can release
that cache without recompressing immutable contents. `Range` and persistence
restore cold pages transiently instead of warming the entire history.
`CompressionStats` reports cold pages, compressed bytes, resident logical bytes
(excluding allocator overhead), and restore operations.

Restore checks the exact backing length, zlib checksum, geometry and VTC1
references. `HistoryChunk.Restore()` lets hosts handle `ErrHistoryCorrupt` before
a read transaction. Ordinary reads panic with that error if private backing is
corrupted: they never silently replace lost text with blanks. External history
bytes still go through the strict public decoder before installation.

On the synthetic 10,000×120 plain-ASCII benchmark, 38 cold pages retain about
4.34 MB including the hot tail and page, versus 22.8 MB before compression.
This is a repeatable fixture result, not a promise about real-world RSS:

```sh
go test . -run '^$' -bench '^BenchmarkColdHistoryRetained10Kx120$' -benchmem -benchtime=1x
```

## Kitty graphics subset

The VT accepts bounded static direct transmissions used by current
`kitten icat --transfer-mode=stream`: PNG, RGB, and RGBA assets; transmit,
transmit-and-display, place, query, and supported delete operations; chunked
Base64 uploads with optional zlib compression; source rectangles; cell extents;
pixel offsets; z-index; and cursor movement policy. Assets and decoded pixels
are bounded by explicit scene and parser limits. File, temporary-file,
shared-memory, animation, composition, relative placement, and
Unicode-placeholder commands remain unsupported.

`Screen` allocates graphics state only after a Kitty APC. Graphics snapshots are
separate from cells and history bytes; VTC1 remains text/history-only. Static
placements move with terminal row scrolling and are clipped by the active
viewport; reflow and relative-placement movement remain unsupported.

### Headless graphics harness

`cmd/graphics-harness` feeds a terminal byte capture into a fresh `Screen` and
writes its active graphics snapshot as a PNG using an internal reference
compositor. It is a development inspection tool, not a terminal emulator.

```sh
go run ./cmd/graphics-harness \
  -input internal/graphicsharness/testdata/demo.apc \
  -output /tmp/graphics-harness.png \
  -cols 4 -rows 4 -pixel-width 4 -pixel-height 4 -scale 64
```

The included demo has an opaque blue background and a semi-transparent red
overlay. Its output is suitable for direct image inspection.

## Checks

```sh
go test ./...
go test ./... -race
go vet ./...
go test ./... -run '^$' -bench='.' -benchmem
```

The storage redesign baseline from issue #9 can be measured independently with:

```sh
go test . -run '^$' \
  -bench '^BenchmarkHistory(Build|Retained)10Kx120$' \
  -benchmem -benchtime=1x -count=1
```

It reports construction allocations and retained heap bytes for plain ASCII,
repeated indexed and RGB styles, high-cardinality RGB churn, wide Unicode, and
styled blanks. History with 16-byte cells retains about 22.8 MB for the plain-ASCII
case, more than 4× below the original 95.3 MB baseline while keeping a bounded
mutable tail. See the [optimization evaluation](docs/storage-optimization.md)
for profiles, chunk-size experiments and rejected low-level techniques.

The compact page primitive can be measured with
`go test ./core -run '^$' -bench '^BenchmarkCompactFrameBuild10Kx120$' -benchmem -benchtime=1x`;
its logical-byte result is deterministic and excludes Go allocator/map overhead.

Scrollback retention uses the same uncompressed logical model: 16 bytes per
stored cell, 4 bytes per row descriptor, 32 bytes per distinct canonical style
in each compact slab (including its default style), and 16 bytes plus UTF-8
string lengths per distinct nonempty exceptional payload. Configure independent
byte and row ceilings with `HistoryConfig.MaxBytes` and `HistoryConfig.MaxRows`.
`History.LogicalBytes`, `HistoryView.LogicalBytes`, and
`HistorySnapshotView.LogicalBytes` expose current usage. This accounting covers
retained scrollback only—not the live primary or alternate screen—and has no
page-granularity allowance. It remains unchanged when sealed history is
compressed.
