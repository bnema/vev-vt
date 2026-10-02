package ansi

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/bnema/vev-vt/core"
)

// genericSource hides the concrete core.Frame type so the renderer must use
// the generic CellSource paths.
type genericSource struct{ f Frame }

func (g genericSource) Columns() int       { return g.f.Columns() }
func (g genericSource) Rows() int          { return g.f.Rows() }
func (g genericSource) Cell(x, y int) Cell { return g.f.Cell(x, y) }

var propertyStyles = []Style{
	DefaultStyle(),
	{Bold: true, Foreground: 1, Background: -1},
	{Italic: true, Foreground: -1, Background: 4},
	{Foreground: -1, Background: -1, HasForegroundRGB: true, ForegroundRGB: core.RGB{R: 9, G: 8, B: 7}},
	{Inverse: true, Foreground: 2, Background: 3},
}

func propertyCell(r *rand.Rand) Cell {
	c := Cell{Rune: rune('a' + r.IntN(6)), Style: propertyStyles[r.IntN(len(propertyStyles))]}
	if r.IntN(12) == 0 {
		if p, err := core.NewCellPayload("e\u0301", "https://example.test/"+string(rune('a'+r.IntN(3)))); err == nil {
			c.Payload = p
		}
	}
	return c
}

func propertyRect(r *rand.Rand, w, h int, kind DamageKind) Damage {
	d := Damage{Kind: kind, X: r.IntN(w), Y: r.IntN(h)}
	d.Width = 1 + r.IntN(w-d.X)
	d.Height = 1 + r.IntN(h-d.Y)
	if kind == DamageScrollUp || kind == DamageScrollDown {
		d.X, d.Width = 0, w
		d.Count = 1 + r.IntN(max(1, d.Height))
	}
	return d
}

func propertyDamage(r *rand.Rand, w, h int) []Damage {
	switch r.IntN(6) {
	case 0:
		return nil
	case 1:
		return []Damage{FullRedraw()}
	case 2:
		return []Damage{propertyRect(r, w, h, DamageText)}
	case 3:
		return []Damage{propertyRect(r, w, h, DamageClear)}
	case 4:
		return []Damage{propertyRect(r, w, h, DamageScrollUp), propertyRect(r, w, h, DamageText)}
	default:
		out := []Damage{propertyRect(r, w, h, DamageScrollDown)}
		for range r.IntN(4) {
			out = append(out, propertyRect(r, w, h, DamageText))
		}
		return out
	}
}

// TestShadowMatchesCloneReference drives one renderer (reused buffers) and a
// reference built from the public clone-based PlanDelta/Commit pair through
// random edits, under-reported damage, scrolls, resets, resizes, discarded
// draws and stale commits. The committed shadows must always agree.
func TestShadowMatchesCloneReference(t *testing.T) {
	for seed := range uint64(40) {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			r := rand.New(rand.NewPCG(seed, 0xa11ce))
			rend := New(Capabilities{})
			var ref Frame
			refHas := false
			w, h := 3+r.IntN(10), 2+r.IntN(8)
			frame := NewFrame(w, h)
			var outstanding *PreparedDraw // last prepared draw, for stale commits

			for step := range 150 {
				// Mutate the caller's single frame in place.
				switch r.IntN(8) {
				case 0:
					w, h = 3+r.IntN(10), 2+r.IntN(8)
					frame = NewFrame(w, h)
				case 1:
					top := r.IntN(h)
					bottom := top + r.IntN(h-top)
					n := 1 + r.IntN(bottom-top+1)
					if r.IntN(2) == 0 {
						frame.ScrollUp(top, bottom, n)
					} else {
						frame.ScrollDown(top, bottom, n)
					}
				}
				for range r.IntN(12) {
					frame.Set(r.IntN(w), r.IntN(h), propertyCell(r))
				}
				damage := propertyDamage(r, w, h)
				reset := r.IntN(15) == 0

				var source CellSource = frame
				switch r.IntN(4) {
				case 0:
					source = &frame
				case 1:
					source = genericSource{frame}
				}

				// Reference plan against a clone-based committed shadow.
				refReset := reset || !refHas
				want, err := PlanDelta(source, damage, ref, refReset)
				if err != nil {
					t.Fatal(err)
				}
				prepared, err := rend.Prepare(source, damage, reset)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(prepared.candidate.Plan, want.Plan) {
					t.Fatalf("step %d: plan %+v, reference %+v", step, prepared.candidate.Plan, want.Plan)
				}
				if prepared.candidate.frame.Width != 0 && !framesEqual(prepared.candidate.frame, want.frame) {
					t.Fatalf("step %d: prepared snapshot differs from reference snapshot", step)
				}

				// Caller keeps mutating after Prepare, before Commit.
				snapshotMutation := r.IntN(3) == 0
				if snapshotMutation {
					frame.Set(r.IntN(w), r.IntN(h), propertyCell(r))
				}

				switch k := r.IntN(10); {
				case k < 6: // commit
					prepared.Commit()
					want.Commit(&ref)
					refHas = refHas || want.frame.Width != 0
					if want.frame.Width == 0 && !refHas {
						// A no-op candidate with no shadow cannot occur: reset snapshots.
						t.Fatalf("step %d: empty plan without committed state", step)
					}
					outstanding = nil
				case k < 8: // discard
					outstanding = &prepared
				default: // stale commit of the previous discarded draw
					if outstanding != nil {
						if hasWork(outstanding.candidate.Plan) {
							outstanding.Commit()
							refHas = false
						} else {
							outstanding.Commit()
						}
						outstanding = nil
					}
				}
				if r.IntN(25) == 0 {
					rend.Reset()
					refHas = false
					ref = Frame{}
				}

				if rend.hasCommitted != refHas {
					t.Fatalf("step %d: hasCommitted=%v, reference %v", step, rend.hasCommitted, refHas)
				}
				if refHas {
					if !framesEqual(rend.committed, ref) {
						t.Fatalf("step %d: committed shadow differs from reference", step)
					}
					if err := rend.committed.CheckInvariants(); err != nil {
						t.Fatalf("step %d: %v", step, err)
					}
					if rend.width != ref.Width || rend.height != ref.Height {
						t.Fatalf("step %d: renderer size %dx%d, reference %dx%d", step, rend.width, rend.height, ref.Width, ref.Height)
					}
				}
			}
		})
	}
}

func hasWork(p DeltaPlan) bool { return p.Snapshot || p.Scroll.Height != 0 || len(p.Spans) != 0 }

func framesEqual(a, b Frame) bool {
	if a.Width != b.Width || a.Height != b.Height {
		return false
	}
	for y := range a.Height {
		for x := range a.Width {
			if !a.Cell(x, y).Equal(b.Cell(x, y)) {
				return false
			}
		}
	}
	return true
}
