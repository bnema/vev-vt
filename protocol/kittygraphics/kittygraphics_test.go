package kittygraphics

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"strconv"
	"testing"

	"github.com/bnema/vev-vt/graphics"
)

func apc(header, payload string) []byte {
	return append([]byte("\x1b_G"+header+";"+payload+"\x1b\\"), []byte{}...)
}

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.SetRGBA(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestParserRecognizesSplitPrefixAndPreservesText(t *testing.T) {
	p := NewParser()
	var events []Event
	for _, b := range []byte("before\x1b_Ga=q,i=7;\x1b\\after") {
		events = append(events, p.Feed([]byte{b})...)
	}
	var textBefore, textAfter []byte
	var command Event
	for _, event := range events {
		if event.Kind == EventText && command.Kind == 0 {
			textBefore = append(textBefore, event.Text...)
		}
		if event.Kind == EventCommand {
			command = event
		}
		if event.Kind == EventText && command.Kind != 0 {
			textAfter = append(textAfter, event.Text...)
		}
	}
	if string(textBefore) != "before" || command.Kind != EventCommand || string(textAfter) != "after" {
		t.Fatalf("events = %#v", events)
	}
	if command.Command.Controls.Action != ActionQuery || command.Command.Controls.ImageID != 7 {
		t.Fatalf("command = %#v", command.Command)
	}
}

func TestSessionCapabilityQueryValidatesWithoutPersisting(t *testing.T) {
	scene := graphics.NewScene(graphics.Limits{})
	session := NewSession(scene)

	result, err := session.Feed(apc("a=q,i=31,t=d,f=24,s=1,v=1", "AAAA"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(result.Bytes()); got != "\x1b_Gi=31;OK\x1b\\" {
		t.Fatalf("response = %q", got)
	}
	if got := scene.Usage(); got != (graphics.Usage{}) {
		t.Fatalf("query persisted graphics state: %#v", got)
	}
}

func TestSessionUsesCurrentKittenChunkFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/kitten-icat-stream-chunk.bin")
	if err != nil {
		t.Fatal(err)
	}
	scene := graphics.NewScene(graphics.Limits{})
	session := NewSession(scene)
	// Feed split boundaries deliberately unrelated to APC boundaries. This
	// models the short writes observed from the current kitten/icat fixture.
	rng := rand.New(rand.NewSource(7))
	var result Result
	for len(data) != 0 {
		n := 1 + rng.Intn(4096)
		if n > len(data) {
			n = len(data)
		}
		part, err := session.Feed(data[:n])
		if err != nil {
			t.Fatal(err)
		}
		result.Events = append(result.Events, part.Events...)
		result.Responses = append(result.Responses, part.Responses...)
		result.Mutations = append(result.Mutations, part.Mutations...)
		data = data[n:]
	}
	if len(result.Mutations) != 1 || result.Mutations[0].Kind != MutationPlacement {
		t.Fatalf("mutations = %#v", result.Mutations)
	}
	if got := scene.Usage(); got.Assets != 1 || got.Placements != 1 || got.DecodedPixels != 40000 {
		t.Fatalf("usage = %#v", got)
	}
	if len(result.Responses) != 0 {
		t.Fatalf("quiet fixture response = %q", result.Bytes())
	}
	if _, ok := session.Image(0); ok {
		t.Fatal("unidentified fixture image was retained as a reusable mapping")
	}
}

func TestDecodeBase64AcceptsStandardAndRaw(t *testing.T) {
	want := []byte{0, 1, 2, 253, 254, 255}
	standard := base64.StdEncoding.EncodeToString(want)
	raw := base64.RawStdEncoding.EncodeToString(want)
	for _, encoded := range []string{standard, raw} {
		got, err := DecodeBase64([]byte(encoded))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("DecodeBase64(%q) = %x, %v", encoded, got, err)
		}
	}
	if _, err := DecodeBase64([]byte("not+base64?")); !errors.Is(err, ErrInvalidBase64) {
		t.Fatalf("invalid base64 error = %v", err)
	}
}

func TestSessionAcceptsRawBase64AndMapsPlacement(t *testing.T) {
	scene := graphics.NewScene(graphics.Limits{})
	session := NewSession(scene)
	payload := base64.RawStdEncoding.EncodeToString([]byte{1, 2, 3, 4})
	result, err := session.Feed(apc("a=t,i=9,f=32,s=1,v=1,q=2", payload))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Responses) != 0 || len(result.Mutations) != 1 {
		t.Fatalf("result = %#v", result)
	}
	asset, ok := session.Image(9)
	if !ok {
		t.Fatal("image mapping missing")
	}
	if _, err := session.Feed(apc("a=p,i=9,p=4,X=5,Y=6,c=1,r=1", "")); err != nil {
		t.Fatal(err)
	}
	placement, ok := session.Placement(9, 4)
	if !ok {
		t.Fatal("placement mapping missing")
	}
	view, ok := scene.Snapshot().Placement(placement)
	if !ok || view.AssetID() != asset || view.Destination() != (graphics.PixelRect{X: 5, Y: 6, Width: 1, Height: 1}) {
		t.Fatalf("placement = %#v, ok=%v", view, ok)
	}
}

func TestPlacementIDsAreScopedToTheirImage(t *testing.T) {
	scene := graphics.NewScene(graphics.Limits{})
	session := NewSession(scene)
	payload := base64.RawStdEncoding.EncodeToString([]byte{1, 2, 3, 4})
	for _, imageID := range []uint64{1, 2} {
		if _, err := session.Feed(apc("a=t,i="+strconv.FormatUint(imageID, 10)+",f=32,s=1,v=1,q=2", payload)); err != nil {
			t.Fatal(err)
		}
		if _, err := session.Feed(apc("a=p,i="+strconv.FormatUint(imageID, 10)+",p=7,q=2", "")); err != nil {
			t.Fatal(err)
		}
	}
	first, ok := session.Placement(1, 7)
	if !ok {
		t.Fatal("first placement missing")
	}
	second, ok := session.Placement(2, 7)
	if !ok || first == second {
		t.Fatalf("placement mapping collision: first=%v second=%v ok=%v", first, second, ok)
	}
	if got := scene.Usage().Placements; got != 2 {
		t.Fatalf("placements = %d, want 2", got)
	}
}

func TestMalformedTruncatedInterleavedAndOversizeAreBounded(t *testing.T) {
	if _, err := ParseCommand([]byte("a=q,a=t;")); !errors.Is(err, ErrDuplicateControl) {
		t.Fatalf("duplicate control error = %v", err)
	}
	if _, err := ParseCommand([]byte("a=q,i=18446744073709551616;")); !errors.Is(err, ErrIntegerOverflow) {
		t.Fatalf("integer overflow error = %v", err)
	}
	if _, err := ParseCommand([]byte("a=q,q=256;")); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("out-of-range quiet error = %v", err)
	}
	if _, err := ParseCommand([]byte("a=q,i=1,I=2;")); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("mixed image selector error = %v", err)
	}

	p := NewParser(Limits{MaxAPCBytes: 8})
	events := p.Feed(apc("a=q", "0123456789"))
	if len(events) != 1 || !errors.Is(events[0].Err, ErrAPCTooLarge) {
		t.Fatalf("oversize events = %#v", events)
	}
	p = NewParser()
	if got := p.Feed([]byte("\x1b_Ga=q;payload")); len(got) != 0 {
		t.Fatalf("truncated feed events = %#v", got)
	}
	got := p.Finish()
	if len(got) != 1 || !errors.Is(got[0].Err, ErrAPCTruncated) {
		t.Fatalf("truncated finish events = %#v", got)
	}

	session := NewSession(graphics.NewScene(graphics.Limits{}))
	first := apc("a=t,i=1,f=32,s=1,v=1,m=1,q=2", "AQ")
	if _, err := session.Feed(first); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Feed(apc("a=t,i=2,m=0,q=2", "ID")); !errors.Is(err, ErrInterleavedUpload) {
		t.Fatalf("interleaved error = %v", err)
	}
	if _, err := session.Feed(apc("a=t,m=0,q=2", "IDBA")); err != nil {
		t.Fatal(err)
	}
}

func TestSessionTruncationAndDeleteAbortPendingUpload(t *testing.T) {
	for _, action := range []string{"finish", "delete"} {
		t.Run(action, func(t *testing.T) {
			scene := graphics.NewScene(graphics.Limits{})
			session := NewSession(scene)
			if _, err := session.Feed(apc("a=T,i=1,f=32,s=1,v=1,m=1,q=2", "AQ")); err != nil {
				t.Fatal(err)
			}
			if action == "finish" {
				if _, err := session.Finish(); err != nil {
					t.Fatalf("Finish error = %v", err)
				}
			} else if _, err := session.Feed(apc("a=d,q=2", "")); err != nil {
				t.Fatal(err)
			}
			if _, err := session.Feed(apc("m=0,q=2", "IDBA")); err == nil {
				t.Fatal("stale continuation unexpectedly succeeded")
			}
			if got := scene.Usage(); got != (graphics.Usage{}) {
				t.Fatalf("stale continuation mutated scene: %#v", got)
			}
		})
	}
}

func TestMalformedPNGDoesNotMutateScene(t *testing.T) {
	good := pngBytes(t, 1, 1)
	scene := graphics.NewScene(graphics.Limits{})
	session := NewSession(scene)
	if _, err := session.Feed(apc("a=t,i=1,f=100,q=2", base64.RawStdEncoding.EncodeToString(good[:len(good)-2]))); !errors.Is(err, ErrInvalidPNG) {
		t.Fatalf("malformed PNG error = %v", err)
	}
	if got := scene.Usage(); got.Assets != 0 || got.Placements != 0 {
		t.Fatalf("malformed PNG mutated scene: %#v", got)
	}
}

func TestImageGeometryRejectsOverflowBeforeByteArithmetic(t *testing.T) {
	controls := Controls{Width: ^uint64(0), HasWidth: true, Height: ^uint64(0), HasHeight: true}
	limits := Limits{MaxDimension: ^uint64(0), MaxDecodedPixels: ^uint64(0)}
	if _, _, err := imageGeometry(nil, FormatRGBA, controls, limits); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("overflowing geometry error = %v", err)
	}
}

func TestChunkedTransmitAndDisplayRetainsDisplayAction(t *testing.T) {
	scene := graphics.NewScene(graphics.Limits{})
	session := NewSession(scene)
	first := apc("a=T,i=1,f=32,s=1,v=1,m=1,q=2", "AQ")
	if _, err := session.Feed(first); err != nil {
		t.Fatal(err)
	}
	last := apc("m=0,q=2", "IDBA")
	result, err := session.Feed(last)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Mutations) != 1 || result.Mutations[0].Kind != MutationPlacement {
		t.Fatalf("mutations = %#v", result.Mutations)
	}
	if got := scene.Usage(); got.Assets != 1 || got.Placements != 1 {
		t.Fatalf("usage = %#v", got)
	}
}

func TestImplicitPlacementIDsStopAtKittyLimit(t *testing.T) {
	session := NewSession(nil)
	session.nextPlacement = MaxKittyID + 1
	if _, ok := session.takePlacementID(1); ok {
		t.Fatal("implicit placement ID exceeded Kitty's uint32 namespace")
	}
}

func TestPlacementFailureEmitsStableProtocolError(t *testing.T) {
	scene := graphics.NewScene(graphics.Limits{})
	session := NewSession(scene)
	if _, err := session.Feed(apc("a=t,i=1,f=32,s=1,v=1,q=2", "AQIDBA")); err != nil {
		t.Fatal(err)
	}
	result, err := session.Feed(apc("a=p,i=1,p=7,w=2,q=0", ""))
	if !errors.Is(err, graphics.ErrInvalidPlacement) {
		t.Fatalf("placement error = %v", err)
	}
	if got := result.Bytes(); string(got) != "\x1b_Gi=1;EINVAL\x1b\\" {
		t.Fatalf("placement response = %q", got)
	}
	if len(result.Mutations) != 0 || scene.Usage().Placements != 0 {
		t.Fatalf("failed placement mutated state: result=%#v usage=%#v", result, scene.Usage())
	}
}

func TestFailedImageReplacementPreservesAssetAndPlacements(t *testing.T) {
	scene := graphics.NewScene(graphics.Limits{MaxEncodedBytes: 4, MaxDecodedPixels: 4})
	session := NewSession(scene)
	if _, err := session.Feed(apc("a=t,i=1,f=32,s=1,v=1,q=2", "AQIDBA")); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Feed(apc("a=p,i=1,p=7,q=2", "")); err != nil {
		t.Fatal(err)
	}
	assetID, ok := session.Image(1)
	if !ok {
		t.Fatal("original image mapping missing")
	}
	placementID, ok := session.Placement(1, 7)
	if !ok {
		t.Fatal("original placement mapping missing")
	}
	before := scene.Snapshot()
	result, err := session.Feed(apc("a=t,i=1,f=32,s=2,v=1,q=0", "AQIDBAUGBwg"))
	if !errors.Is(err, graphics.ErrEncodedBudget) {
		t.Fatalf("replacement error = %v", err)
	}
	if got := result.Bytes(); string(got) != "\x1b_Gi=1;E2BIG\x1b\\" {
		t.Fatalf("replacement response = %q", got)
	}
	if got, ok := session.Image(1); !ok || got != assetID {
		t.Fatalf("replacement changed image mapping: id=%v ok=%v want=%v", got, ok, assetID)
	}
	if got, ok := session.Placement(1, 7); !ok || got != placementID {
		t.Fatalf("replacement changed placement mapping: id=%v ok=%v want=%v", got, ok, placementID)
	}
	if scene.Snapshot().Generation() != before.Generation() || scene.Usage() != before.Usage() {
		t.Fatalf("failed replacement changed scene: before=%#v after=%#v", before.Usage(), scene.Usage())
	}
	if _, ok := scene.Snapshot().Placement(placementID); !ok {
		t.Fatal("failed replacement removed original placement")
	}
}

func TestFailedDisplayedImageReplacementPreservesOldPlacement(t *testing.T) {
	scene := graphics.NewScene(graphics.Limits{})
	session := NewSession(scene)
	if _, err := session.Feed(apc("a=T,i=1,f=32,s=1,v=1,q=2", "AQIDBA")); err != nil {
		t.Fatal(err)
	}
	assetID, ok := session.Image(1)
	if !ok {
		t.Fatal("original image mapping missing")
	}
	placementID, ok := session.Placement(1, 1)
	if !ok {
		t.Fatal("original placement mapping missing")
	}

	result, err := session.Feed(apc("a=T,i=1,f=32,s=1,v=1,w=2,q=0", "AQIDBA"))
	if !errors.Is(err, graphics.ErrInvalidPlacement) {
		t.Fatalf("replacement placement error = %v", err)
	}
	if got := result.Bytes(); string(got) != "\x1b_Gi=1;EINVAL\x1b\\" {
		t.Fatalf("replacement response = %q", got)
	}
	if got, ok := session.Image(1); !ok || got != assetID {
		t.Fatalf("failed displayed replacement changed image: id=%v ok=%v want=%v", got, ok, assetID)
	}
	if got, ok := session.Placement(1, 1); !ok || got != placementID {
		t.Fatalf("failed displayed replacement changed placement: id=%v ok=%v want=%v", got, ok, placementID)
	}
	if got := scene.Usage(); got.Assets != 1 || got.Placements != 1 {
		t.Fatalf("failed displayed replacement usage = %#v", got)
	}
}

func TestDeleteSelectedImagePlacementsAndImageNumberNamespace(t *testing.T) {
	scene := graphics.NewScene(graphics.Limits{})
	session := NewSession(scene)
	payload := base64.RawStdEncoding.EncodeToString([]byte{1, 2, 3, 4})
	for range 2 {
		if _, err := session.Feed(apc("a=t,I=7,f=32,s=1,v=1,q=2", payload)); err != nil {
			t.Fatal(err)
		}
	}
	if got := scene.Usage(); got.Assets != 2 || got.Placements != 0 {
		t.Fatalf("numbered uploads collapsed namespaces: %#v", got)
	}
	if _, err := session.Feed(apc("a=p,I=7,p=2,q=2", "")); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Feed(apc("a=p,i=1,p=1,q=2", "")); err != nil {
		t.Fatal(err)
	}
	if got := scene.Usage(); got.Placements != 2 {
		t.Fatalf("placements = %#v", got)
	}
	if _, err := session.Feed(apc("a=d,d=i,i=1,q=2", "")); err != nil {
		t.Fatal(err)
	}
	if got := scene.Usage(); got.Assets != 2 || got.Placements != 1 {
		t.Fatalf("selected delete affected unrelated scene state: %#v", got)
	}
	if _, err := session.Feed(apc("a=d,d=I,I=7,q=2", "")); err != nil {
		t.Fatal(err)
	}
	if got := scene.Usage(); got.Assets != 1 || got.Placements != 0 {
		t.Fatalf("image-number delete = %#v", got)
	}
	if _, ok := session.Image(1); !ok {
		t.Fatal("image ID namespace was removed by image-number delete")
	}
}

func FuzzParserNeverPanics(f *testing.F) {
	f.Add([]byte("text\x1b_Ga=q;\x1b\\"))
	f.Add([]byte("\x1b_Ga=T,m=1;AAAA\x1b\\\x1b_Gm=0;\x1b\\"))
	f.Fuzz(func(t *testing.T, data []byte) {
		p := NewParser(Limits{MaxAPCBytes: 1024, MaxPayloadBytes: 512, MaxUploadBytes: 512})
		_ = p.Feed(data)
		_ = p.Finish()
	})
}

func FuzzSessionNeverPanics(f *testing.F) {
	f.Add([]byte("\x1b_Ga=t,i=1,f=32,s=1,v=1;AQIDBA==\x1b\\"))
	f.Add([]byte("\x1b_Ga=T,f=100,m=1;AAAA\x1b\\\x1b_Gm=0;AAAA\x1b\\"))
	f.Fuzz(func(t *testing.T, data []byte) {
		s := NewSession(graphics.NewScene(graphics.Limits{}), Limits{
			MaxAPCBytes: 4096, MaxPayloadBytes: 2048, MaxUploadBytes: 2048,
			MaxDimension: 128, MaxDecodedPixels: 4096,
		})
		_, _ = s.Feed(data)
		_, _ = s.Finish()
	})
}
