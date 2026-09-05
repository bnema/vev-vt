# Storage optimization evaluation (#19)

## Decision

Keep Go-owned slices/maps, 256-row default chunks and standard-library zlib.
Do not add pooling, mmap/decommit, SIMD or physical page alignment. Accept one
measured allocation fix: bound initial mutable-tail capacity by the actual
`width × ChunkRows`, in addition to the existing byte/preallocation ceilings.

This is an evaluation, not a claim that unimplemented techniques were benchmarked.
No assembly, unsafe code, OS-specific allocator, dependency or background worker
was introduced.

## Measurements

September 4, 2026; Linux amd64, AMD Ryzen 5 7535HS, Go 1.27.1. Chunk-size figures
are two one-iteration samples, so small timing differences are not significant.
All variants use 10,000 rows × 120 columns with repeated plain text.

| Chunk rows | Before: ms/op | Before: allocated MB/op | After capacity fix: ms/op | After: allocated MB/op |
| --- | ---: | ---: | ---: | ---: |
| 32 | 184–197 | 1,089 | 120–127 | 148.0 |
| 64 | 161–163 | 555.9 | 126–130 | 146.9 |
| 128 | 137–139 | 290.0 | 125–129 | 146.9 |
| 256 | 125 | 157.0 | 124–128 | 148.5 |

The old fixed initial capacity overallocated each short tail. A history configured
for four three-cell rows retained 3,408,840 bytes after one append. The corrected
capacity retains 2,248 bytes in the same benchmark. A regression test pins the
initial capacity to at most twelve cells for that case.

For the default 256-row configuration after the fix:

- Uncompressed plain-ASCII history retains **22.8 MB**, versus the original
  **95.3 MB** baseline: the required 4× reduction still holds.
- Two cold-maintenance passes retain **4.34 MB**, with 38 cold pages and 173,190
  compressed bytes. The newest page and mutable tail remain hot.
- This is synthetic retained Go heap, not a real-daemon RSS measurement.

A three-iteration CPU/allocation profile before the capacity fix attributed about
86.8% of allocated bytes to `reserveTailRow`. CPU samples were concentrated in
compact cell writes, canonical dictionaries and chunk construction. The short
profile supports identifying the allocation source, not predicting production
latency or claiming a precise speedup.

## Options considered

| Option | Result | Reason |
| --- | --- | --- |
| Geometry-bounded tail preallocation | Accepted | Large reproducible allocation reduction for small chunks; no ownership changes. |
| Change default logical chunk rows | Rejected for now | Corrected throughput samples overlap. 256 rows uses fewer allocations/dictionaries than smaller chunks. |
| Pool tail/page memory | Not justified yet | The immediate cause was excessive requested capacity. Sealed pages may outlive History through borrowed views, making recycling ownership-sensitive; pools can also retain cold memory. |
| mmap/decommit | Not justified | Go GC already releases dropped page references; no measured syscall/RSS benefit supports platform-specific backing, finalization and failure paths. |
| SIMD | Not justified | The measured work is semantic cell writes and dictionary lookup, not a demonstrated bulk transform suited to SIMD. Native copy/clear remains the baseline. |
| Fixed physical page alignment | Rejected | No measured benefit. Chunk rows are logical grouping, not an OS page-size commitment. |

Cold random-access restore is cached until later idle visits. Streaming history
search and persistence use transient restores, so scanning does not make every
cold page resident. Restoring the 16×80 test page, including cache release, measured
about 123 microseconds and 92.8 KB allocated; use the benchmark below for current
results rather than treating that sample as a latency guarantee.

## Reproduce

```sh
go test . -run '^$' -bench '^(BenchmarkHistoryChunkSize|BenchmarkSmallHistoryRetained)$' -benchtime=1x -count=2 -benchmem
go test . -run '^$' -bench '^(BenchmarkHistoryRetained10Kx120|BenchmarkColdHistoryRetained10Kx120)/plain-ascii$' -benchtime=1x -benchmem
go test . -run '^$' -bench '^BenchmarkColdHistoryRestore$' -benchtime=100ms -benchmem
go test . -o /tmp/vev-vt-profile.test -run '^$' -bench '^BenchmarkHistoryBuild10Kx120/plain-ascii$' -benchtime=3x -cpuprofile /tmp/vev-vt-storage.cpu -memprofile /tmp/vev-vt-storage.mem
go tool pprof -top /tmp/vev-vt-profile.test /tmp/vev-vt-storage.cpu
go tool pprof -top -alloc_space /tmp/vev-vt-profile.test /tmp/vev-vt-storage.mem
```

Keep the 4× original retained-memory gate, ownership/identity tests, strict codec
validation, race tests and 386 portability checks for future experiments. A
production workload/RSS study remains necessary before selecting any rejected
low-level technique.
