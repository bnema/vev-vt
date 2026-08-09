package vt_test

import (
	"bytes"
	"embed"
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"strings"
	"testing"
)

//go:embed testdata/vevs-v4-restore.golden.hex
var fixtures embed.FS

func TestVEVSV4FixtureIsDeterministic(t *testing.T) {
	encoded, err := fixtures.ReadFile("testdata/vevs-v4-restore.golden.hex")
	if err != nil {
		t.Fatal(err)
	}
	data, err := hex.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(data) < 16 || !bytes.Equal(data[:4], []byte("VEVS")) {
		t.Fatalf("fixture header = %x, want VEVS", data[:min(len(data), 4)])
	}
	if version := binary.BigEndian.Uint16(data[4:6]); version != 4 {
		t.Fatalf("fixture version = %d, want 4", version)
	}
	if bodyLength := binary.BigEndian.Uint32(data[8:12]); int(bodyLength) != len(data)-16 {
		t.Fatalf("fixture body length = %d, want %d", bodyLength, len(data)-16)
	}
	if got := binary.BigEndian.Uint32(data[12:16]); got != crc32.ChecksumIEEE(data[16:]) {
		t.Fatalf("fixture CRC = %#x, want %#x", got, crc32.ChecksumIEEE(data[16:]))
	}
	if got := bytes.Count(data, []byte("VTH1")); got != 3 {
		t.Fatalf("embedded VTH3 marker count = %d, want 3", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
