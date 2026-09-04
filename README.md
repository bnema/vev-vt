# vev-vt

A Go library for reading terminal output and keeping track of what should appear
on screen. It powers [vev](https://github.com/bnema/vev).

Feed it the bytes from a shell or command. It handles text, colors, cursor
movement, scrolling and resizing. You can then read the screen, save its history,
or use the `ansi` package to draw it in a terminal.

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

- Write to a screen from one goroutine, or protect access with your own lock.
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
| `vev-vt/graphics` | Read supported terminal images and their positions. |

[Supported image features →](docs/graphics.md)

## Development

```sh
go test ./...
go test ./... -race
go vet ./...
```

[Storage benchmarks and design decisions →](docs/storage-optimization.md)
