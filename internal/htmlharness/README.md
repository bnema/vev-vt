# HTML browser harness

This development-only Playwright harness verifies the embedded HTML frontend. It
loads `html/terminal.css` and `html/browser/terminal.js` directly; no production
server or transport is involved.

## Locked environment

`package-lock.json` pins Playwright 1.62.1 and these browser builds:

| Engine | Browser version | Playwright revision |
|---|---:|---:|
| Chromium | 151.0.7922.34 | 1234 |
| Firefox | 153.0 | 1538 |
| WebKit | 26.5 | 2336 |

Run locally when the host supports all browser builds:

```sh
npm ci
npm run test:browser:install
npm run test:browser
```

Use the matching official container on unsupported Linux distributions:

```sh
npm run test:browser:docker
```

CI installs the same locked browsers and runs the full matrix on Ubuntu.

## Evidence

The three-engine container matrix passes typed snapshot and row application,
hostile-text safety, complete validation before mutation, IME composition,
non-text keys, synchronous default-prevention decisions, pointer, wheel, resize,
focus, typed themes, lifecycle cleanup, and a self-only `script-src`/`style-src`
CSP. The CSP fixture uses external static assets and reports no policy violation.

The Chromium reference performance gate applies ten sustained 120×40 snapshots,
fifty complete-row updates, and one 240×80 snapshot. The recorded implementation
run produced:

| Workload | Recorded | Gate |
|---|---:|---:|
| 120×40 snapshot p95 | 28.2 ms | < 100 ms |
| 120-column row replacement p95 | 0.5 ms | < 33 ms |
| 240×80 snapshot | 68.3 ms | < 500 ms |
| Final visible cells/rows | 19,200 / 80 | exactly bounded by the frame |

Performance numbers are reference evidence, not universal device guarantees.
The functional matrix and serial Chromium performance run write separate JSON
reports under `test-results/`. They are local/CI artifacts rather than committed
files.

## Accessibility scope

Automated checks cover a named group, a labeled keyboard-focusable text input,
visible focus styling, synchronized plain-text output, and reduced-motion CSS.
The visual grid is `aria-hidden` to avoid exposing thousands of fragmented cell
nodes; assistive technology receives the bounded plain-text representation.
`role="application"` is not used, browser zoom remains available, and browser or
system shortcuts are not prevented unless the synchronous consumer decision
requests it.

No claim of full screen-reader equivalence is made. A product consumer should
run its own assistive-technology acceptance pass with the surrounding labels,
shortcut policy, announcements, and session behavior before presenting the
frontend as a complete terminal replacement.

## Inherited output limits

The current core model drops combining marks and does not coalesce ZWJ
sequences. The harness verifies composed browser input but does not claim that
the renderer can recover combining or ZWJ output absent from `core.Frame`.
