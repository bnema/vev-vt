# Performance work

How to measure CPU time and allocations before and after a change, and how to
check that the change kept behavior.

## Workflow

1. Pick the benchmark that covers the code you change (table below).
2. Record a baseline on the base branch.
3. Make the change, then record the same benchmark again.
4. Compare with `benchstat`; keep only wins that are stable across runs.
5. Run the safety checks below before you commit.

```sh
# Baseline, from a worktree of the base branch
go test ./html -run '^$' -bench '^BenchmarkRenderer$' -benchmem -count=10 > /tmp/old.txt
# After the change
go test ./html -run '^$' -bench '^BenchmarkRenderer$' -benchmem -count=10 > /tmp/new.txt
benchstat /tmp/old.txt /tmp/new.txt
```

`benchstat` comes from `go install golang.org/x/perf/cmd/benchstat@latest`. Use
`-count=10` so it can report variation; a `~` in its output means no
significant change.

## Benchmarks

| Area | Package | Benchmarks |
|---|---|---|
| VT parsing and screen writes | `.` | `BenchmarkScreenPrintableASCII`, `BenchmarkScreenMixedUTF8`, `BenchmarkScreenCSIHeavy`, `BenchmarkScreenShellRedrawBurst`, `BenchmarkScreenFullscreenScrollRegion`, `BenchmarkScreenKittyAPC` |
| Damage capture and resize | `.` | `BenchmarkScreenCaptureDamage`, `BenchmarkScreenResizeReflowViewport` |
| History build, views and storage | `.` | `BenchmarkHistoryBuild10Kx120`, `BenchmarkHistoryView10KRows*`, `BenchmarkHistoryRetained10Kx120`, see [storage-optimization.md](storage-optimization.md) |
| ANSI renderer | `./ansi` | `BenchmarkRendererFullFrameDraw`, `BenchmarkRendererIncrementalOneCell`, `BenchmarkRendererFragmentedDamage`, `BenchmarkRendererBroadRegularDamage`, `BenchmarkRendererIncrementalNoBytePrepareCommit` |
| HTML renderer | `./html` | `BenchmarkRenderer` (`snapshot-*` and `one-row-*` at 80x24, 120x40, 240x80) |

`one-row-*` is the common interactive case: one changed line in a full frame.
It must stay cheaper than `snapshot-*` at the same size.

## Profiles

Use a CPU or memory profile to find where the time or bytes go:

```sh
go test ./html -run '^$' -bench '^BenchmarkRenderer$/one-row-240x80' -benchmem \
  -cpuprofile /tmp/cpu.out -memprofile /tmp/mem.out
go tool pprof -top /tmp/cpu.out
go tool pprof -sample_index=alloc_space -top /tmp/mem.out
go tool pprof -http=:0 /tmp/cpu.out   # flame graph in the browser
```

To see why a value escapes to the heap:

```sh
go build -gcflags=-m ./html 2>&1 | grep -v inlin
```

Tests can pin an allocation result with `testing.AllocsPerRun`; several
packages already do, for example `TestCopyFromDoesNotAllocateOnceSized` in
`core`.

## Safety checks

A faster change must produce the same output:

```sh
go test ./...                    # includes TestDifferentialOracle
go test -race ./...
go vet ./... && staticcheck ./...
```

`TestDifferentialOracle` digests the output of every layer (VT, history,
graphics, ANSI, HTML) for seeded workloads. A performance change must leave
`testdata/oracle/golden.txt` unchanged. The property tests in `ansi` and `html`
compare reused-buffer renderers with clone-based references through random
edits, aborts and resets.

When a change reuses buffers, check:

- Nothing returned to a caller still points into a reused buffer.
- Retained scratch is bounded, so one huge frame does not pin memory.
- Reused slices of strings or pointers are cleared, so they do not keep old
  values alive.

## Browser rendering

For the HTML frontend, the browser's style, layout and paint usually cost more
than the Go side. The Playwright performance gate in
[`internal/htmlharness`](../internal/htmlharness/README.md) bounds sustained
snapshot and row work in Chromium:

```sh
npx playwright test internal/htmlharness/performance.spec.js --project=chromium
```

Measure user-visible scrolling in the host application, with a real GPU and the
user's zoom level: headless Chromium does not reproduce compositor costs.
