# vev-vt

`github.com/bnema/vev-vt` is a VT terminal engine for Go with a
frontend-neutral core and concrete output packages. It provides the terminal
screen, scrollback history, immutable snapshots,
stable history chunks, VTH3 history bytes, and the reusable cell/frame model.
The `ansi` package turns core frames and damage into transactional ANSI output.
The `html` package prepares typed browser updates, and `html/browser` provides a
safe interactive DOM adapter without owning transport or terminal-input policy.

## Packages

- The module root (`github.com/bnema/vev-vt`) owns VT parsing, screen state,
  history, snapshots, callbacks, and the public model aliases (`Cell`, `Style`,
  `RGB`, `Frame`, `Damage`, and `RuneWidth`).
- `github.com/bnema/vev-vt/core` owns the frontend-neutral cell, style, frame,
  damage, and width implementation. It has no terminal, renderer, transport,
  or application dependencies.
- `github.com/bnema/vev-vt/ansi` is the concrete ANSI output package. It consumes
  core frames and damage; it does not define a renderer-backend interface.
- `github.com/bnema/vev-vt/html` prepares transactional typed snapshots and
  complete-row browser updates, structural CSS, and generic terminal themes.
- `github.com/bnema/vev-vt/html/browser` embeds the dependency-free DOM runtime
  and strictly decodes neutral browser events. It does not own HTTP, WebSocket,
  PTY encoding, authentication, sessions, or application bindings.
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

`Snapshot`, `HistorySnapshotView`, and `Row` methods provide owned captures or
copies. `BorrowedRow`, `Frame.Row`, and `HistoryView.Range` expose borrowed
storage with the lifetimes documented by their APIs; borrowed storage must not
be mutated or retained beyond its documented lifetime. Sealed `HistoryChunk`
identity is stable for the lifetime of a view.

## Compatibility

VTH3 history bytes are canonical and are decoded strictly, including malformed,
truncated, and trailing input rejection. VEVS is an application-owned outer
snapshot envelope and is intentionally not implemented here.

The module has no production dependencies. `github.com/stretchr/testify` is
used only by the test suite. Keep the public v0.x API and byte formats immutable
once released; behavior changes require explicit versioning and compatibility
evidence.

## HTML frontend

The HTML renderer compares every row with its committed shadow; damage values are
non-authoritative hints. `Prepare` permits one outstanding draw and requires an
explicit `Commit` or `Abort`. `Reset` invalidates retained prepared draws.
Updates are immutable, schema-versioned JSON-compatible values. Complete-row
replacement preserves wide-cell atomicity, and scroll damage uses a safe
snapshot fallback. `html.DefaultLimits()` documents the default 1,000,000-cell,
10,000-row, 64 MiB generated-update, and 65,536-style bounds.

The browser adapter builds DOM nodes with `textContent` and fixed classes. It
provides a labeled input proxy, synchronized plain-text accessible output,
typed CSS themes, IME-aware text input, keys, paste, pointer, wheel, resize, and
focus events. A synchronous consumer callback decides default prevention.
Consumers remain responsible for transport and mapping events to terminal bytes
or application actions. `browser.DefaultEventLimits()` documents the default
2 MiB event, 64 KiB text, 1 MiB paste, and 10,000×10,000 geometry bounds.

```go
renderer, err := html.New(html.Options{})
if err != nil {
    return err
}
prepared, err := renderer.Prepare(frame, damage, reset, html.Cursor{
    Row: cursorRow, Column: cursorColumn, Visible: true,
})
if err != nil {
    return err
}
if err := send(prepared.JSON()); err != nil {
    _ = prepared.Abort()
    return err
}
return prepared.Commit()
```

Embed or serve `html.Stylesheet()` and `browser.JavaScript()` from a
consumer-owned application. The runtime supports a self-only `style-src` and
`script-src` CSP in the pinned browser harness; dynamic colors are set through
validated numeric DOM style properties. The current core model drops combining
marks and does not coalesce ZWJ sequences, so rendered output inherits those
limits even though browser IME input remains composition-aware.

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
separate from cells and history bytes; VTH3 remains text/history-only. Static
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
npm ci
npm run test:browser:install
npm run test:browser
# Reproducible three-engine fallback on unsupported Linux hosts:
npm run test:browser:docker
```

Playwright 1.62.1 and its Chromium, Firefox, and WebKit revisions are pinned by
`package-lock.json`. The matching Playwright container provides the reproducible
fallback when host libraries cannot run one of those browser builds. Production
Go packages retain the module's standard-library-only dependency boundary apart
from `vev-vt/core`.
