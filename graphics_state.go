package vt

import (
	"github.com/bnema/vev-vt/graphics"
	"github.com/bnema/vev-vt/protocol/kittygraphics"
)

// screenGraphicsState owns the protocol adapter and scene for one VT screen
// buffer. It is allocated only after the first Kitty graphics APC reaches the
// screen, so ordinary text writes do not pay for graphics state.
type screenGraphicsState struct {
	scene *graphics.Scene
	kitty *kittygraphics.Session
}

func newScreenGraphicsState() *screenGraphicsState {
	scene := graphics.NewScene(graphics.Limits{})
	return &screenGraphicsState{
		scene: scene,
		kitty: kittygraphics.NewSession(scene),
	}
}

func (g *screenGraphicsState) snapshot() *graphics.Snapshot {
	if g == nil || g.scene == nil {
		return nil
	}
	return g.scene.Snapshot()
}

// clearPlacements applies the ordinary full-screen erase policy without
// discarding uploaded image mappings. The protocol session owns those
// mappings, so the operation goes through Kitty's delete-all-visible command
// rather than mutating the scene directly.
func (g *screenGraphicsState) clearPlacements() {
	if g == nil || g.kitty == nil {
		return
	}
	g.kitty.AbortPendingUpload()
	_, _ = g.kitty.Process(kittygraphics.Command{Controls: kittygraphics.Controls{
		Action:    kittygraphics.ActionDelete,
		HasAction: true,
		Delete:    kittygraphics.DeleteAll,
		HasDelete: true,
		Quiet:     kittygraphics.QuietAll,
		HasQuiet:  true,
	}})
}

func (s *Screen) graphicsClearPlacements() {
	if s == nil {
		return
	}
	if s.graphics != nil {
		s.graphics.clearPlacements()
	}
	s.kittyPendingDisplay = nil
}

func (s *Screen) abortKittyPendingDisplay() {
	if s == nil {
		return
	}
	s.kittyPendingDisplay = nil
	if s.graphics != nil && s.graphics.kitty != nil {
		s.graphics.kitty.AbortPendingUpload()
	}
}

func (s *Screen) abortAllKittyPending() {
	if s == nil {
		return
	}
	s.abortKittyPendingDisplay()
	if state := s.alternate; state != nil {
		state.kittyPendingDisplay = nil
		if state.graphics != nil && state.graphics.kitty != nil {
			state.graphics.kitty.AbortPendingUpload()
		}
	}
}

// GraphicsSnapshot returns an immutable snapshot of the active screen
// buffer's graphics scene, or nil before graphics has been used. The returned
// reference remains valid after subsequent Screen mutations.
func (s *Screen) GraphicsSnapshot() *graphics.Snapshot {
	if s == nil || s.graphics == nil {
		return nil
	}
	return s.graphics.snapshot()
}
