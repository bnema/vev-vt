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
// mappings, so the operation goes through its delete-all-placements command
// rather than mutating the scene directly.
func (g *screenGraphicsState) clearPlacements() {
	if g == nil || g.kitty == nil {
		return
	}
	_, _ = g.kitty.Process(kittygraphics.Command{Controls: kittygraphics.Controls{
		Action:    kittygraphics.ActionDelete,
		HasAction: true,
		Delete:    kittygraphics.DeleteAllPlacements,
		HasDelete: true,
		Quiet:     kittygraphics.QuietAll,
		HasQuiet:  true,
	}})
}

func (s *Screen) graphicsClearPlacements() {
	if s != nil && s.graphics != nil {
		s.graphics.clearPlacements()
	}
}

// GraphicsScene returns the active screen buffer's graphics scene, or nil
// before graphics has been used. Calling it never allocates graphics state.
func (s *Screen) GraphicsScene() *graphics.Scene {
	if s == nil || s.graphics == nil {
		return nil
	}
	return s.graphics.scene
}

// Graphics is a concise alias for GraphicsScene.
func (s *Screen) Graphics() *graphics.Scene { return s.GraphicsScene() }

// GraphicsSession returns the active screen buffer's Kitty graphics adapter,
// or nil before graphics has been used.
func (s *Screen) GraphicsSession() *kittygraphics.Session {
	if s == nil || s.graphics == nil {
		return nil
	}
	return s.graphics.kitty
}

// KittyGraphics is an adapter-named alias for GraphicsSession.
func (s *Screen) KittyGraphics() *kittygraphics.Session { return s.GraphicsSession() }

// GraphicsSnapshot returns an immutable snapshot of the active screen
// buffer's graphics scene, or nil before graphics has been used.
func (s *Screen) GraphicsSnapshot() *graphics.Snapshot {
	if s == nil || s.graphics == nil {
		return nil
	}
	return s.graphics.snapshot()
}
