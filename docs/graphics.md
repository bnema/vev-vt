# Terminal images

[Back to the README](../README.md)

vev-vt supports a subset of the Kitty graphics protocol for static images sent
directly through terminal output, including the form used by:

```sh
kitten icat --transfer-mode=stream image.png
```

Your application reads the result through `Screen.GraphicsSnapshot()` and chooses
how to display it. This library does not open a window.

## Supported

- PNG, RGB and RGBA images, with explicit resource limits.
- Direct image transmission, display, placement, queries and supported deletion.
- Chunked Base64 data, with optional zlib compression.
- Cropping, cell-based placement, pixel offsets and z-index.
- Movement and clipping as terminal rows scroll.

Images are stored separately from text history. The `graphics` package contains
the image model; `protocol/kittygraphics` reads Kitty commands and updates it.
Graphics state is allocated only when a screen receives a graphics command.

## Not supported

File or shared-memory transfers, animation, image composition, relative
placements and Unicode placeholders are not implemented. Image placements do
not support resize reflow.

## Inspect a capture without a display

The development harness reads captured terminal bytes and writes a PNG:

```sh
go run ./cmd/graphics-harness \
  -input internal/graphicsharness/testdata/demo.apc \
  -output /tmp/graphics-harness.png \
  -cols 4 -rows 4 -pixel-width 4 -pixel-height 4 -scale 64
```

The included example produces a blue background with a translucent red overlay.
This harness is an inspection tool, not an interactive terminal emulator.
