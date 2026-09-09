# vev-vt

A Go library for reading terminal output and keeping track of what should appear
on screen. It powers [vev](https://github.com/bnema/vev).

Feed it the bytes from a shell or command. It handles text, colors, cursor
movement, scrolling and resizing. You can then read the screen, save its history,
or use the `ansi` package to draw it in a terminal.
The `html` package prepares typed browser updates, and `html/browser` provides a
safe interactive DOM adapter without owning transport or terminal-input policy.

This library does **not** start processes or manage a PTY. Your application does
that and passes the output to vev-vt.

## Install

Requires Go 1.27 or newer.

```sh
go get github.com/bnema/vev-vt
```

**Alpha software:** APIs and saved-history formats can change between versions.
The current history format does not read older VTH3 data.

## Read terminal output

```go
package main

import (
    "fmt"

    vt "github.com/bnema/vev-vt"
)

func main() {
    screen := vt.NewScreen(80, 24)
    screen.Write([]byte("Hello, \x1b[31mworld\x1b[0m!"))

    // Colors and escape sequences are processed, not included in the text.
    for x := 0; x < 13; x++ {
        fmt.Printf("%c", screen.Cell(x, 0).Rune)
    }
    fmt.Println() // Hello, world!
}
```

Use `screen.Resize(columns, rows)` when the terminal size changes.
Use `screen.Snapshot()` when you need a copy that stays unchanged as new output
arrives.

## Keep scrollback

Scrollback is the text that has moved above the visible screen.

```go
// Your application chooses these limits; vev-vt has no default budget.
config := vt.HistoryConfig{
    MaxBytes: 20_000_000, // At most 20 MB of history data.
    MaxRows:  5_000,      // Optional: also keep at most 5,000 lines.
}
screen := vt.NewScreenWithHistory(80, 24, config)
```

vev-vt enforces the limits your application supplies. The PTY transports bytes;
it does not store scrollback. The oldest lines are removed when a supplied limit
is reached. Limits apply to each
screen separately and exclude the visible screen. The byte limit measures
uncompressed history data, **not total process memory**.

[History guide →](docs/history.md): limit settings, saving/restoring history and
optional compression during idle time.

## A few rules

- Serialize `Write`, `Resize`, `Snapshot`, reads and history mutations in one
  goroutine, or protect them with your own lock.
- Rows returned by the API are copies. Changing one does not change the screen.
- Use `vt.DefaultStyle()` for terminal-default colors, not `vt.Style{}`.
- Callbacks run during `screen.Write`; keep them short.

[API ownership and styles →](docs/ownership.md)

## Packages

| Package | Use it for |
| --- | --- |
| `vev-vt` | Parse terminal output, read the screen and manage history. |
| `vev-vt/core` | Work with cells, styles and writable grids. |
| `vev-vt/ansi` | Render a screen or grid as ANSI terminal output. |
| `vev-vt/html` | Prepare transactional browser updates, structural CSS and terminal themes. |
| `vev-vt/html/browser` | Embed the DOM runtime and decode neutral browser events. |
| `vev-vt/graphics` | Read supported terminal images and their positions. |

[Supported image features →](docs/graphics.md)

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
or application actions. Clipboard text is preserved unchanged, including
control bytes, so consumers forwarding paste events to a PTY must apply their
required framing or filtering policy. `browser.DefaultEventLimits()` documents
the default 8 MiB event, 64 KiB text, 1 MiB paste, and 10,000×10,000 geometry
bounds.

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

## Development

```sh
go test ./...
go test ./... -race
go vet ./...
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

[Storage benchmarks and design decisions →](docs/storage-optimization.md)
