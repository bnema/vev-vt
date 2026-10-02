package vt_test

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"hash"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	vt "github.com/bnema/vev-vt"
	"github.com/bnema/vev-vt/ansi"
	core "github.com/bnema/vev-vt/core"
	"github.com/bnema/vev-vt/graphics"
	"github.com/bnema/vev-vt/html"
)

// The differential oracle drives seeded terminal workloads through the public
// pipeline (VT parsing, damage, history, codecs, ANSI and HTML renderers) and
// hashes every observable result at checkpoints. Performance work must keep
// the golden file byte-identical; behavior changes must regenerate it with
// -oracle.update and justify the difference.
var (
	oracleUpdate = flag.Bool("oracle.update", false, "rewrite testdata/oracle/golden.txt")
	oracleDump   = flag.String("oracle.dump", "", "write full plain-text transcripts to this directory")
)

const (
	oracleScenarios  = 36
	oracleSteps      = 240
	oracleCheckEvery = 20
	oracleGolden     = "testdata/oracle/golden.txt"
)

type oracleScenario struct {
	seed      uint64
	cols      int
	rows      int
	history   *vt.HistoryConfig
	byteSplit bool
	evictHook bool
}

func oracleScenarioFor(i int) oracleScenario {
	r := rand.New(rand.NewPCG(uint64(i), 0x5eed))
	sc := oracleScenario{seed: uint64(i), cols: 8 + r.IntN(110), rows: 3 + r.IntN(40)}
	switch i % 4 {
	case 1:
		sc.history = &vt.HistoryConfig{MaxRows: 50 + r.IntN(400), ChunkRows: 1 + r.IntN(16)}
	case 2:
		sc.history = &vt.HistoryConfig{MaxBytes: uint64(64<<10 + r.IntN(512<<10)), ChunkRows: 1 + r.IntN(64)}
	case 3:
		sc.history = &vt.HistoryConfig{MaxRows: 2000, MaxBytes: 4 << 20}
	}
	sc.byteSplit = i%5 == 0
	sc.evictHook = i%3 == 0
	return sc
}

type oracleRun struct {
	t       *testing.T
	sc      oracleScenario
	r       *rand.Rand
	screen  *vt.Screen
	ansiR   *ansi.Renderer
	ansi256 *ansi.Renderer
	htmlR   *html.Renderer
	replay  *vt.Screen
	// replay256 receives the ANSI-256 renderer output. Colors are projected,
	// so only text and wide-cell structure are compared.
	replay256 *vt.Screen
	// mirror reproduces the vev daemon consumer: one core.Frame reused and
	// mutated in place between draws, so renderer shadows must not alias it.
	mirror core.Frame
	// hashes holds one digest per observable concern, so an intentional
	// change to one layer (for example ANSI bytes) leaves the others'
	// golden columns untouched and auditable.
	hashes map[string]hash.Hash
	dump   *bufio.Writer
	events []string
}

// oracleConcerns are the golden columns, in output order. "input" covers the
// generated workload and VT callbacks; the others cover one layer each.
var oracleConcerns = []string{"input", "vt", "graphics", "history", "ansi", "ansi256", "html"}

func (o *oracleRun) emit(concern, format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	h := o.hashes[concern]
	if h == nil {
		o.t.Fatalf("unknown oracle concern %q", concern)
	}
	h.Write([]byte(line))
	h.Write([]byte{'\n'})
	if o.dump != nil {
		// bufio.Writer keeps the first error; the deferred Flush reports it.
		_, _ = fmt.Fprintf(o.dump, "%s: %s\n", concern, line)
	}
}

func (o *oracleRun) digest() string {
	var b strings.Builder
	for i, concern := range oracleConcerns {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(concern)
		b.WriteByte('=')
		b.WriteString(hex.EncodeToString(o.hashes[concern].Sum(nil))[:12])
	}
	return b.String()
}

func formatCell(c core.Cell) string {
	return fmt.Sprintf("%q|%+v|%q|%q|%t", c.Rune, c.Style, c.Payload.Grapheme(), c.Payload.Hyperlink(), c.Continuation)
}

// formatRow is a compact exact digest of a row: every Cell field contributes.
// Full text is only produced for dumps, where it is needed for diagnosis.
func formatRow(row []core.Cell) string {
	h := sha256.New()
	var buf []byte
	for _, c := range row {
		buf = fmt.Appendf(buf[:0], "%d|%+v|%s|%s|%t;", c.Rune, c.Style, c.Payload.Grapheme(), c.Payload.Hyperlink(), c.Continuation)
		h.Write(buf)
	}
	return hex.EncodeToString(h.Sum(nil)[:12])
}

func rowText(row []core.Cell) string {
	var b strings.Builder
	for _, c := range row {
		if c.Continuation {
			continue
		}
		if g := c.Payload.Grapheme(); g != "" {
			b.WriteString(g)
		} else if c.Rune != 0 {
			b.WriteRune(c.Rune)
		}
	}
	return b.String()
}

func (o *oracleRun) genChunk() []byte {
	r := o.r
	var b bytes.Buffer
	for range 1 + r.IntN(6) {
		switch op := r.IntN(100); {
		case op < 22:
			words := []string{"hello", "world", "vev", "ls -la", "❯", "foo/bar", "λx", "0123456789"}
			for range 1 + r.IntN(8) {
				b.WriteString(words[r.IntN(len(words))])
				b.WriteByte(' ')
			}
			if r.IntN(2) == 0 {
				b.WriteString("\r\n")
			}
		case op < 30:
			uni := []string{"界", "🙂", "é", "e\u0301", "한국어", "👍🏽", "a\u200db", "ﬀ"}
			for range 1 + r.IntN(10) {
				b.WriteString(uni[r.IntN(len(uni))])
			}
		case op < 42:
			b.WriteString(randomSGR(r))
		case op < 50:
			fmt.Fprintf(&b, "\x1b[%d;%dH", 1+r.IntN(o.sc.rows+2), 1+r.IntN(o.sc.cols+2))
		case op < 56:
			seqs := []string{"\x1b[K", "\x1b[1K", "\x1b[2K", "\x1b[J", "\x1b[2J", "\x1b[3@", "\x1b[2P", "\x1b[L", "\x1b[2M", "\x1b[4X", "\x1b[S", "\x1b[2T", "\x1bM", "\x1bD", "\x1bE", "\t", "\b"}
			b.WriteString(seqs[r.IntN(len(seqs))])
		case op < 64:
			for range 1 + r.IntN(3*o.sc.rows) {
				fmt.Fprintf(&b, "line %d %s\r\n", r.IntN(1000), strings.Repeat("=", r.IntN(o.sc.cols+5)))
			}
		case op < 68:
			top := 1 + r.IntN(o.sc.rows)
			bottom := top + r.IntN(o.sc.rows-top+1)
			fmt.Fprintf(&b, "\x1b[%d;%dr\x1b[%d;1H", top, bottom, bottom)
			for range 1 + r.IntN(6) {
				b.WriteString("region\n")
			}
			if r.IntN(2) == 0 {
				b.WriteString("\x1b[r")
			}
		case op < 71:
			if r.IntN(2) == 0 {
				b.WriteString("\x1b[?1049h")
			} else {
				b.WriteString("\x1b[?1049l")
			}
		case op < 74:
			fmt.Fprintf(&b, "\x1b]8;;https://example.invalid/%d\x1b\\link\x1b]8;;\x1b\\", r.IntN(9))
		case op < 76:
			fmt.Fprintf(&b, "\x1b]0;title %d\x07", r.IntN(99))
		case op < 81:
			b.WriteString(randomKitty(r))
		case op < 84:
			queries := []string{"\x1b[6n", "\x1b[c", "\x1b[?2004$p", "\x1b[5n"}
			b.WriteString(queries[r.IntN(len(queries))])
		case op < 86:
			if r.IntN(2) == 0 {
				b.WriteString("\x1b[?7l")
			} else {
				b.WriteString("\x1b[?7h")
			}
		case op < 91:
			// High-cardinality truecolor burst.
			for range 1 + r.IntN(40) {
				fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm#", r.IntN(256), r.IntN(256), r.IntN(256))
			}
			b.WriteString("\x1b[0m")
		case op < 94:
			// Styled blanks and erases inherit the current background.
			fmt.Fprintf(&b, "\x1b[48;5;%dm\x1b[K   \x1b[0m", r.IntN(256))
		default:
			fmt.Fprintf(&b, "\r\x1b[K❯ abc\x1b[90m suggestion\x1b[39m\r\x1b[%dC", r.IntN(8))
		}
	}
	return b.Bytes()
}

func randomSGR(r *rand.Rand) string {
	parts := []string{"0", "1", "2", "3", "4", "4:3", "5", "7", "9", "22", "23", "24", "27", "39", "49", "59"}
	switch r.IntN(6) {
	case 0:
		return fmt.Sprintf("\x1b[%dm", 30+r.IntN(8))
	case 1:
		return fmt.Sprintf("\x1b[38;5;%d;48;5;%dm", r.IntN(256), r.IntN(256))
	case 2:
		return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r.IntN(256), r.IntN(256), r.IntN(256))
	case 3:
		return fmt.Sprintf("\x1b[58;2;%d;%d;%d;4:%dm", r.IntN(256), r.IntN(256), r.IntN(256), r.IntN(6))
	default:
		return "\x1b[" + parts[r.IntN(len(parts))] + "m"
	}
}

func randomKitty(r *rand.Rand) string {
	switch r.IntN(4) {
	case 0:
		w, h := 1+r.IntN(4), 1+r.IntN(4)
		pixels := make([]byte, w*h*4)
		for i := range pixels {
			pixels[i] = byte(r.IntN(256))
		}
		enc := encodeBase64(pixels, r.IntN(2) == 0)
		return fmt.Sprintf("\x1b_Ga=T,i=%d,f=32,s=%d,v=%d,c=%d,r=%d,q=%d;%s\x1b\\", 1+r.IntN(4), w, h, 1+r.IntN(3), 1+r.IntN(2), r.IntN(3), enc)
	case 1:
		// Chunked transmission.
		pixels := make([]byte, 4*4*4)
		for i := range pixels {
			pixels[i] = byte(r.IntN(256))
		}
		enc := encodeBase64(pixels, true)
		mid := (len(enc) / 8) * 4
		return fmt.Sprintf("\x1b_Ga=t,i=%d,f=32,s=4,v=4,m=1;%s\x1b\\\x1b_Gm=0;%s\x1b\\\x1b_Ga=p,i=%d\x1b\\", 5+r.IntN(3), enc[:mid], enc[mid:], 5+r.IntN(3))
	case 2:
		return fmt.Sprintf("\x1b_Ga=d,d=%s\x1b\\", []string{"a", "A", "i", "I"}[r.IntN(4)])
	default:
		// Invalid base64 must be rejected identically.
		return "\x1b_Ga=T,f=32,s=1,v=1;!!!\x1b\\"
	}
}

func encodeBase64(data []byte, padded bool) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var b strings.Builder
	for i := 0; i < len(data); i += 3 {
		var chunk [3]byte
		n := copy(chunk[:], data[i:])
		v := uint(chunk[0])<<16 | uint(chunk[1])<<8 | uint(chunk[2])
		for j := range 4 {
			if j <= n {
				b.WriteByte(alphabet[(v>>(18-6*j))&63])
			} else if padded {
				b.WriteByte('=')
			}
		}
	}
	return b.String()
}

func (o *oracleRun) write(data []byte) {
	if o.sc.byteSplit {
		for i := range data {
			o.screen.Write(data[i : i+1 : i+1])
		}
		return
	}
	for len(data) > 0 {
		n := 1 + o.r.IntN(len(data))
		if o.r.IntN(3) != 0 {
			n = len(data)
		}
		o.screen.Write(data[:n:n])
		data = data[n:]
	}
}

func (o *oracleRun) maybeResize(step int) {
	if o.r.IntN(40) != 0 {
		return
	}
	cols, rows := 8+o.r.IntN(110), 3+o.r.IntN(40)
	o.emit("input", "step %d resize %dx%d", step, cols, rows)
	o.screen.Resize(cols, rows)
}

func (o *oracleRun) checkScreen() {
	s := o.screen
	o.emit("vt", "screen %dx%d cursor=%d,%d vis=%t title=%q alt=%t", s.Columns(), s.Rows(), s.CursorRow(), s.CursorCol(), s.CursorVisible(), s.TerminalTitle(), s.AltScreenActive())
	snap := s.Snapshot()
	o.emit("vt", "modes %+v cursor %+v next=%d", snap.Modes(), snap.Cursor(), snap.NextRowID())
	for y := range s.Rows() {
		o.emit("vt", "row %d id=%d bound=%+v %s %q", y, s.RowID(y), snap.Bound(y), formatRow(s.RowCells(y)), rowText(s.RowCells(y)))
		snapRow := snap.Row(y)
		if formatRow(snapRow) != formatRow(s.RowCells(y)) {
			o.t.Fatalf("seed %d: snapshot row %d differs from live row", o.sc.seed, y)
		}
	}
	o.emit("vt", "bounds %+v", s.LineBounds())
}

func (o *oracleRun) checkGraphics() {
	g := o.screen.GraphicsSnapshot()
	if g == nil {
		o.emit("graphics", "graphics nil")
		return
	}
	o.emit("graphics", "graphics gen=%d usage=%+v", g.Generation(), g.Usage())
	for _, a := range g.Assets() {
		blob := a.Blob()
		sum := sha256.Sum256(blob.Encoded)
		o.emit("graphics", "asset fmt=%v %dx%d px=%d sha=%x", blob.Format, blob.Width, blob.Height, blob.DecodedPixels, sum[:8])
	}
	for _, p := range g.Placements() {
		o.emit("graphics", "placement %+v", p.Spec())
	}
	o.emit("graphics", "fragments %+v", g.VisibleCellFragments(graphics.CellRect{Width: int64(o.screen.Columns()), Height: int64(o.screen.Rows())}))
}

func (o *oracleRun) checkHistory(deep bool) {
	h := o.screen.History()
	if h == nil {
		return
	}
	o.emit("history", "history len=%d cells=%d bytes=%d next=%d stats=%+v", h.Len(), h.Cells(), h.LogicalBytes(), h.NextRowID(), h.CompressionStats())
	if !deep {
		return
	}
	snapView := h.SnapshotView()
	tail, err := vt.MarshalHistoryTail(snapView)
	if err != nil {
		o.t.Fatalf("seed %d: MarshalHistoryTail: %v", o.sc.seed, err)
	}
	sum := sha256.Sum256(tail)
	o.emit("history", "snapview len=%d chunks=%d tail=%x", snapView.Len(), snapView.ChunkCount(), sum)
	var sealed [][]byte
	for i := range snapView.ChunkCount() {
		blob, err := vt.MarshalHistoryChunk(snapView.Chunk(i))
		if err != nil {
			o.t.Fatalf("seed %d: MarshalHistoryChunk: %v", o.sc.seed, err)
		}
		sum := sha256.Sum256(blob)
		o.emit("history", "chunk %d %x", i, sum)
		sealed = append(sealed, blob)
	}
	view := h.View()
	var rows []string
	var texts []string
	if err := view.Range(func(row []core.Cell) bool {
		rows = append(rows, formatRow(row))
		texts = append(texts, rowText(row))
		return true
	}); err != nil {
		o.t.Fatalf("seed %d: history Range: %v", o.sc.seed, err)
	}
	for i, row := range rows {
		o.emit("history", "hrow %d id=%d bound=%+v %s %q", i, view.RowID(i), view.Bound(i), row, texts[i])
		if got := formatRow(view.Row(i)); got != row {
			o.t.Fatalf("seed %d: history Row(%d) differs from Range", o.sc.seed, i)
		}
		dst := make([]core.Cell, view.RowWidth(i))
		view.CopyRow(i, dst)
		if formatRow(dst) != row {
			o.t.Fatalf("seed %d: history CopyRow(%d) differs from Range", o.sc.seed, i)
		}
	}
	restored, err := vt.HistoryFromBlobs(h.Limits(), sealed, tail)
	if err != nil {
		o.t.Fatalf("seed %d: HistoryFromBlobs: %v", o.sc.seed, err)
	}
	rview := restored.View()
	if rview.Len() != view.Len() || restored.NextRowID() != h.NextRowID() {
		o.t.Fatalf("seed %d: restored history len=%d next=%d want len=%d next=%d", o.sc.seed, rview.Len(), restored.NextRowID(), view.Len(), h.NextRowID())
	}
	for i, row := range rows {
		if formatRow(rview.Row(i)) != row || rview.RowID(i) != view.RowID(i) || rview.Bound(i) != view.Bound(i) {
			o.t.Fatalf("seed %d: restored history row %d differs", o.sc.seed, i)
		}
	}
	transcript, err := o.screen.RecoveryTranscriptSnapshot().Marshal()
	if err != nil {
		o.t.Fatalf("seed %d: recovery transcript: %v", o.sc.seed, err)
	}
	tsum := sha256.Sum256(transcript)
	o.emit("history", "transcript %x", tsum)
	recovered, err := vt.NewScreenWithRecoveryTranscript(o.screen.Columns(), o.screen.Rows(), h.Limits(), sealed, tail, transcript)
	if err != nil {
		o.t.Fatalf("seed %d: NewScreenWithRecoveryTranscript: %v", o.sc.seed, err)
	}
	rh := recovered.History()
	o.emit("history", "recovered len=%d next=%d", rh.Len(), rh.NextRowID())
	if rh.Len() > 0 {
		rv := rh.View()
		o.emit("history", "recovered last %s", formatRow(rv.Row(rv.Len()-1)))
	}
}

func (o *oracleRun) render(step int) {
	s := o.screen
	damage := append([]core.Damage(nil), s.Damage()...)
	capture := s.CaptureDamage()
	if len(capture.Damage) != len(damage) {
		o.t.Fatalf("seed %d: CaptureDamage length %d want %d", o.sc.seed, len(capture.Damage), len(damage))
	}
	o.emit("vt", "damage %+v", damage)

	if o.mirror.Width != s.Columns() || o.mirror.Height != s.Rows() {
		o.mirror = core.NewFrame(s.Columns(), s.Rows())
	}
	for y := range s.Rows() {
		for x := range s.Columns() {
			o.mirror.Set(x, y, s.Cell(x, y))
		}
	}
	var source core.CellSource = o.mirror
	switch o.sc.seed % 3 {
	case 1:
		source = &o.mirror
	case 2:
		source = s // generic CellSource path
	}
	prepared, err := o.ansiR.Prepare(source, damage, false)
	if err != nil {
		o.t.Fatalf("seed %d step %d: ansi Prepare: %v", o.sc.seed, step, err)
	}
	out := prepared.Bytes()
	o.emit("ansi", "ansi %q", out)
	if o.r.IntN(7) == 0 {
		// Discarded draw: renderer must keep its committed shadow and damage
		// must remain pending for the next draw.
		o.emit("ansi", "ansi discard")
	} else {
		prepared.Commit()
		o.replay = o.applyReplay(o.replay, out, step, true)
		if !s.AcknowledgeDamage(capture.Generation) {
			o.t.Fatalf("seed %d: acknowledge failed", o.sc.seed)
		}
	}
	other, err := o.ansi256.Draw(source, damage)
	if err != nil {
		o.t.Fatalf("seed %d: ansi256 Draw: %v", o.sc.seed, err)
	}
	o.emit("ansi256", "ansi256 %q", other)
	o.replay256 = o.applyReplay(o.replay256, other, step, false)

	snap := s.Snapshot()
	cur := snap.Cursor()
	var htmlSource core.CellSource = o.mirror
	if o.sc.seed%3 == 1 {
		htmlSource = snap
	}
	hp, err := o.htmlR.Prepare(htmlSource, damage, false, html.Cursor{Row: cur.Row, Column: cur.Col, Visible: cur.Visible, Style: html.CursorStyle(cur.Style), StyleSet: cur.StyleSet})
	if err != nil {
		o.emit("html", "html err %v", err)
		return
	}
	o.emit("html", "html %s", hp.JSON())
	if o.r.IntN(6) == 0 {
		if err := hp.Abort(); err != nil {
			o.t.Fatalf("seed %d: html abort: %v", o.sc.seed, err)
		}
		o.emit("html", "html abort")
	} else if err := hp.Commit(); err != nil {
		o.t.Fatalf("seed %d: html commit: %v", o.sc.seed, err)
	}
}

// applyReplay feeds committed ANSI output to an independent VT and requires
// the visible grid to match the source. This semantic check does not depend on
// the golden file.
func (o *oracleRun) applyReplay(replay *vt.Screen, out []byte, step int, styles bool) *vt.Screen {
	s := o.screen
	if replay == nil || replay.Columns() != s.Columns() || replay.Rows() != s.Rows() {
		replay = vt.NewScreen(s.Columns(), s.Rows())
	}
	replay.Write(out)
	for y := range s.Rows() {
		for x := range s.Columns() {
			want, got := s.Cell(x, y), replay.Cell(x, y)
			if want.Rune != got.Rune || want.Continuation != got.Continuation || (styles && !want.Style.Equal(got.Style)) {
				o.t.Fatalf("seed %d step %d: ANSI replay (styles=%t) mismatch at (%d,%d): got %s want %s", o.sc.seed, step, styles, x, y, formatCell(got), formatCell(want))
			}
		}
	}
	return replay
}

func runOracleScenario(t *testing.T, idx int) string {
	sc := oracleScenarioFor(idx)
	o := &oracleRun{t: t, sc: sc, r: rand.New(rand.NewPCG(sc.seed, 0xdecaf)), hashes: map[string]hash.Hash{}}
	for _, concern := range oracleConcerns {
		o.hashes[concern] = sha256.New()
	}
	if *oracleDump != "" {
		if err := os.MkdirAll(*oracleDump, 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(*oracleDump, fmt.Sprintf("scenario-%02d.txt", idx)))
		if err != nil {
			t.Fatal(err)
		}
		o.dump = bufio.NewWriter(f)
		defer func() {
			if err := errors.Join(o.dump.Flush(), f.Close()); err != nil {
				t.Errorf("write oracle dump: %v", err)
			}
		}()
	}
	if sc.history != nil {
		o.screen = vt.NewScreenWithHistory(sc.cols, sc.rows, *sc.history)
	} else {
		o.screen = vt.NewScreen(sc.cols, sc.rows)
	}
	o.screen.OnResponse = func(b []byte) { o.events = append(o.events, fmt.Sprintf("response %q", b)) }
	o.screen.OnBell = func() { o.events = append(o.events, "bell") }
	if sc.evictHook {
		// The callback owns each row: retain it, verify it is never rewritten by
		// later evictions, then scribble on it to prove VT kept its own copy.
		var retained []core.Cell
		var retainedDigest string
		o.screen.OnLineEvicted = func(row []core.Cell) {
			if retained != nil && formatRow(retained) != retainedDigest {
				t.Fatalf("seed %d: evicted row storage was reused after the callback", sc.seed)
			}
			digest := formatRow(row)
			o.events = append(o.events, "evicted "+digest+" "+rowText(row))
			if prev := retained; prev != nil {
				for i := range prev {
					prev[i] = core.Cell{Rune: 'X'}
				}
			}
			retained, retainedDigest = row, digest
		}
	}
	o.ansiR = ansi.New(ansi.Capabilities{SynchronizedOutput: idx%2 == 0})
	o.ansi256 = ansi.NewWithColorProfile(ansi.Capabilities{}, ansi.ColorProfileANSI256)
	var err error
	o.htmlR, err = html.New(html.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var historyConfig vt.HistoryConfig
	if sc.history != nil {
		historyConfig = *sc.history
	}
	o.emit("input", "scenario %d %dx%d history=%+v byteSplit=%t evictHook=%t", idx, sc.cols, sc.rows, historyConfig, sc.byteSplit, sc.evictHook)

	var digests []string
	for step := range oracleSteps {
		o.maybeResize(step)
		chunk := o.genChunk()
		o.emit("input", "step %d write %q", step, chunk)
		o.write(chunk)
		for _, e := range o.events {
			o.emit("input", "%s", e)
		}
		o.events = o.events[:0]
		o.render(step)
		if step%oracleCheckEvery == oracleCheckEvery-1 {
			o.checkScreen()
			o.checkGraphics()
			o.checkHistory(step%(2*oracleCheckEvery) == 2*oracleCheckEvery-1)
			digests = append(digests, fmt.Sprintf("%02d/%03d %s", idx, step, o.digest()))
		}
	}
	return strings.Join(digests, "\n")
}

// TestDifferentialOracle compares every checkpoint against
// testdata/oracle/golden.txt. Each golden line holds one digest per concern
// (see oracleConcerns), so an intentional output change shows which layers
// moved. Regenerate with -oracle.update only for a deliberate behavior change,
// on a full run, and justify the changed columns; diagnose a divergence by
// running -oracle.dump=DIR on both trees and diffing the scenario transcript.
// Under -race only every sixth scenario runs: scenarios are single-goroutine,
// so the detector adds cost without coverage.
func TestDifferentialOracle(t *testing.T) {
	if testing.Short() {
		t.Skip("differential oracle skipped in -short mode")
	}
	got := make([]string, oracleScenarios)
	t.Run("scenarios", func(t *testing.T) {
		for i := range oracleScenarios {
			t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
				if raceEnabled && i%6 != 0 {
					t.Skip("reduced oracle under -race")
				}
				t.Parallel()
				got[i] = runOracleScenario(t, i)
			})
		}
	})
	if t.Failed() {
		return
	}
	ran := 0
	for _, digests := range got {
		if digests != "" {
			ran++
		}
	}
	if *oracleUpdate {
		if ran != oracleScenarios {
			t.Fatalf("-oracle.update needs every scenario; only %d of %d ran (remove -run filters and -race)", ran, oracleScenarios)
		}
		if err := os.MkdirAll(filepath.Dir(oracleGolden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(oracleGolden, []byte(strings.Join(got, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	golden, err := os.ReadFile(oracleGolden)
	if err != nil {
		t.Fatalf("read golden (run with -oracle.update on a trusted baseline): %v", err)
	}
	want := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(golden)), "\n") {
		if len(line) < 3 {
			t.Fatalf("malformed golden line %q (regenerate with -oracle.update on a trusted baseline)", line)
		}
		want[line[:2]] = append(want[line[:2]], line)
	}
	for i, digests := range got {
		if digests == "" {
			continue
		}
		compareOracleScenario(t, fmt.Sprintf("%02d", i), strings.Split(digests, "\n"), want[fmt.Sprintf("%02d", i)])
	}
}

func compareOracleScenario(t *testing.T, id string, got, want []string) {
	t.Helper()
	for i := range min(len(got), len(want)) {
		if got[i] == want[i] {
			continue
		}
		gotFields, wantFields := strings.Fields(got[i]), strings.Fields(want[i])
		var moved []string
		for j := 1; j < min(len(gotFields), len(wantFields)); j++ {
			if gotFields[j] != wantFields[j] {
				moved = append(moved, strings.SplitN(gotFields[j], "=", 2)[0])
			}
		}
		t.Fatalf("scenario %s diverged at checkpoint %s in %v\n got  %s\n want %s\nrerun with -oracle.dump=DIR on both trees and diff the transcript", id, gotFields[0], moved, got[i], want[i])
	}
	if len(got) != len(want) {
		t.Fatalf("scenario %s checkpoint count differs: got %d want %d", id, len(got), len(want))
	}
}
