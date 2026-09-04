# Working with history

[Back to the README](../README.md)

History contains the lines that have scrolled off the visible screen. Each
`Screen` has its own history; the library does not share one limit across panes
or sessions.

## Choose limits

Your application chooses the budget and passes it in `HistoryConfig`.
vev-vt provides no default byte or line budget. It only measures the stored
history and removes the oldest lines when a supplied limit is reached.
The PTY transports terminal bytes; the application's history policy is separate.

| Setting | Meaning |
| --- | --- |
| `MaxBytes` | Maximum uncompressed history data. Zero omits this ceiling. This is not a process-RSS limit. |
| `MaxRows` | Maximum lines. Set to zero with positive `MaxBytes` for a byte-only limit. |
| Both limits zero | Disable history. |
| Positive `MaxRows`, zero `MaxBytes` | Limit only the number of lines. No byte budget is invented. |
| `ChunkRows` | Internal grouping size, 1–256. Usually leave it at zero for the default, 256. |

The visible primary and alternate screens do not count toward these limits.
Grouping rows does not grant extra space beyond the limit.

If settings come from user input, call `config.Validate()` before creating a
screen. Invalid settings make constructors panic. To change an existing history,
call `history.SetLimits(config)`: it returns an error for invalid input and
removes excess old rows immediately after a valid reduction.

Use `history.Limits()` to read the effective settings, `Len()` for the number of
lines and `LogicalBytes()` for the amount charged to the byte limit.

## Read without exposing writable storage

- `history.View()` captures the current contents. Later writes cannot change it.
- `view.Row(y)` returns a copy of a line.
- `view.Cell(x, y)` reads one cell; `RowWidth(y)` gets its width without copying it.
- `view.CopyRow(y, destination)` reuses a slice supplied by your application.
- `view.Range(callback)` walks lines in order. Each callback receives its own row.

Older groups of rows, called chunks, can be shared between views. Their identity
stays stable, which lets applications avoid saving or processing them repeatedly.

## Save and restore

For a single history blob, use `MarshalHistory` and `UnmarshalHistory`.
For incremental snapshots, use `SnapshotView`, `MarshalHistoryChunk` and
`MarshalHistoryTail`; restore them with `HistoryFromBlobs`.

A snapshot includes row identities and line-wrap information as well as cells.
The copied snapshot tail is reusable: asking for `Tail()` repeatedly does not
copy it again.

The format is **VTC1**. Older VTH3 data is rejected, not migrated. Check decode
errors and do not overwrite existing saved data after a failed restore. Application
snapshot envelopes such as vev's VEVS format are outside this library.

Each blob is limited to 12,000 rows and 1,920,000 cells. Exceptional payload
records and strings have a separate 64 MiB limit. Chunk-by-chunk persistence
avoids putting an entire large history in one blob. The decoder checks the
complete blob before allocating decoded grids, including references, duplicate
IDs, string limits, truncation and extra trailing bytes.

## Compress during idle time

Compression is optional. From the same goroutine or lock that owns the history,
call `history.CompressIdle(1)` on an idle timer to visit at most one old page per
call. The library does not start a timer or goroutine for you.

A page is compressed only after two visits with no read between them. The newest
sealed page and the unfinished tail stay uncompressed. Reading an older page
restores it automatically. This changes memory use, not how many lines are kept.

Restored pages are cached until later idle visits. Searches through `Range` and
history serialization restore pages temporarily instead of caching every page.
Use `CompressionStats()` to inspect compressed bytes, cold pages and restores.
Its resident-byte estimate excludes Go allocator and map overhead.

Corrupted compressed data is never replaced with blank text. To handle errors
before reading, call `chunk.Restore()` and check for `ErrHistoryCorrupt`. An
ordinary read panics with that error if the library's private backing is corrupt.
Restore checks length, checksum, page size and the usual VTC1 validation rules.

## How byte accounting works

These implementation details matter when interpreting `LogicalBytes()`:

| Item | Charged size |
| --- | ---: |
| Stored cell | 16 bytes |
| Row descriptor | 4 bytes |
| Distinct style in a page, including default | 32 bytes |
| Distinct nonempty exceptional payload | 16 bytes plus its UTF-8 strings |

Styles and exceptional values are shared within a page. Compression does not
change these charges. The wire encoding is separate: ordinary cells use 9 bytes,
and cells with an exceptional payload add a 4-byte reference. Only semantic
values are serialized, never raw Go page memory or internal handles.

[Benchmarks and optimization choices](storage-optimization.md)
