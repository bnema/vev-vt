package vt

import (
	"strconv"
	"strings"
)

// Kitty keyboard progressive enhancement flags.
// https://sw.kovidgoyal.net/kitty/keyboard-protocol/#progressive-enhancement
const (
	KittyKeyboardDisambiguate    = 1 << 0
	KittyKeyboardReportEvents    = 1 << 1
	KittyKeyboardAlternateKeys   = 1 << 2
	KittyKeyboardAllKeysAsEscape = 1 << 3
	KittyKeyboardAssociatedText  = 1 << 4

	kittyKeyboardFlagMask = 1<<5 - 1
	// maxKittyKeyboardStack bounds the per-screen flag stack. When full, a
	// push evicts the oldest entry, as the protocol recommends.
	maxKittyKeyboardStack = 16
)

// kittyKeyboardStack is one screen's stack of keyboard enhancement flags. The
// current flags are the top entry, or zero when the stack is empty.
type kittyKeyboardStack []int

func (k kittyKeyboardStack) current() int {
	if len(k) == 0 {
		return 0
	}
	return k[len(k)-1]
}

func (k *kittyKeyboardStack) push(flags int) {
	if len(*k) == maxKittyKeyboardStack {
		copy(*k, (*k)[1:])
		*k = (*k)[:len(*k)-1]
	}
	*k = append(*k, flags&kittyKeyboardFlagMask)
}

func (k *kittyKeyboardStack) pop(n int) {
	*k = (*k)[:len(*k)-min(n, len(*k))]
}

// set applies CSI = flags ; mode u. Mode 1 replaces, 2 sets bits, 3 clears
// bits. With an empty stack the result becomes the single entry.
func (k *kittyKeyboardStack) set(flags, mode int) {
	cur := k.current()
	switch mode {
	case 1:
		cur = flags
	case 2:
		cur |= flags
	case 3:
		cur &^= flags
	default:
		return
	}
	cur &= kittyKeyboardFlagMask
	if len(*k) == 0 {
		*k = append(*k, cur)
		return
	}
	(*k)[len(*k)-1] = cur
}

// KittyKeyboardFlags reports the kitty keyboard enhancement flags currently
// requested by the application on the active (main or alternate) screen.
func (s *Screen) KittyKeyboardFlags() int { return s.kittyKeyboard.current() }

// applyKittyKeyboard handles CSI u sequences with a ?, >, <, or = prefix. It
// reports whether params belonged to the kitty keyboard protocol.
func (s *Screen) applyKittyKeyboard(params string) bool {
	if params == "" {
		return false
	}
	prefix, rest := params[0], params[1:]
	switch prefix {
	case '?':
		if rest != "" {
			return true
		}
		resp := make([]byte, 0, 8)
		resp = append(resp, "\x1b[?"...)
		resp = strconv.AppendInt(resp, int64(s.kittyKeyboard.current()), 10)
		resp = append(resp, 'u')
		s.respond(resp)
	case '>':
		flags, _ := kittyKeyboardParams(rest)
		s.kittyKeyboard.push(flags)
	case '<':
		n, _ := kittyKeyboardParams(rest)
		s.kittyKeyboard.pop(max(n, 1))
	case '=':
		flags, mode := kittyKeyboardParams(rest)
		s.kittyKeyboard.set(flags, max(mode, 1))
	default:
		return false
	}
	return true
}

// kittyKeyboardParams parses up to two decimal parameters. Missing or
// malformed values are zero.
func kittyKeyboardParams(params string) (first, second int) {
	a, b, _ := strings.Cut(params, ";")
	return parseCSIInt(a), parseCSIInt(b)
}
