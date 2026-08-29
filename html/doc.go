// Package html prepares safe, transactional browser updates from the
// frontend-neutral github.com/bnema/vev-vt/core model.
//
// Renderer state is single-owner and is not internally synchronized. Prepare
// allows exactly one outstanding draw. The consumer must call Commit only
// after accepting the immutable update, or Abort to preserve the committed
// shadow. Reset invalidates any outstanding draw.
//
// Damage values are non-authoritative hints: every row is compared with the
// committed shadow, so mutations outside reported damage remain observable.
// Updates replace complete rows to preserve wide-cell atomicity. Scroll damage
// uses a full snapshot until browser measurements justify a more complex plan.
//
// The package emits typed data and structural CSS; it never converts terminal
// text to markup. Package browser owns safe DOM application and neutral input
// events. HTTP, WebSocket, sessions, PTY encoding, and application policy are
// consumer responsibilities.
package html
