#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
if grep -q '^replace ' "$root/go.mod"; then
	echo 'standalone module must not commit a replace directive' >&2
	exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/consumer"
cat >"$tmp/consumer/go.mod" <<EOF
module example.com/vev-vt-consumer

go 1.26

require github.com/bnema/vev-vt v0.0.0

replace github.com/bnema/vev-vt => $root
EOF
cat >"$tmp/consumer/main.go" <<'EOF'
package main

import (
	"fmt"
	"os"

	vt "github.com/bnema/vev-vt"
	"github.com/bnema/vev-vt/ansi"
)

func main() {
	screen := vt.NewScreen(4, 1)
	screen.Write([]byte("ok"))
	output, err := ansi.New(ansi.Capabilities{}).Draw(screen.Frame.Clone(), []vt.Damage{vt.FullRedraw()})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(output) == 0 {
		fmt.Fprintln(os.Stderr, "consumer received empty ANSI output")
		os.Exit(1)
	}
}
EOF

(
	cd "$tmp/consumer"
	GOWORK=off go test ./...
)
