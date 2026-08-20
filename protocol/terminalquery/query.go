// Package terminalquery provides bounded parsers for terminal capability
// probes. Probe responses are terminal input, not application input: callers
// can remove only the two exact responses while replaying every unrelated byte.
package terminalquery

import "bytes"

const (
	// KittyGraphicsQuery asks a terminal whether it understands the Kitty
	// graphics protocol. The image id is deliberately reserved for probing and
	// the query carries no image payload.
	KittyGraphicsQuery = "\x1b_Gi=31,s=1,v=1,a=q;\x1b\\"
	// DeviceAttributesQuery is the primary DA1 query. It is paired with the
	// Kitty query so a graphics declaration requires both terminal identity and
	// protocol support.
	DeviceAttributesQuery = "\x1b[c"

	maxProbeResponseBytes = 256
)

// Probe incrementally recognizes the bounded responses to KittyGraphicsQuery
// and DeviceAttributesQuery. Feed returns bytes that are not recognized probe
// responses in their original order. A response may be split across any
// number of calls.
type Probe struct {
	pending []byte
	kitty   bool
	da1     bool
}

// Feed consumes one terminal input fragment and returns unrelated input. The
// returned slice is owned by the Probe and is invalidated by the next call.
func (p *Probe) Feed(data []byte) []byte {
	if p == nil || len(data) == 0 {
		return append([]byte(nil), data...)
	}
	p.pending = append(p.pending, data...)
	return p.scan(false)
}

// Finish flushes an incomplete or unrelated prefix without recognizing any
// further response. It is used when the bounded probe deadline expires.
func (p *Probe) Finish() []byte {
	if p == nil {
		return nil
	}
	return p.scan(true)
}

// Ready reports whether both independent capability responses were observed.
func (p *Probe) Ready() bool { return p != nil && p.kitty && p.da1 }

// KittyGraphics reports whether the Kitty graphics response was observed.
func (p *Probe) KittyGraphics() bool { return p != nil && p.kitty }

// DA1 reports whether a primary device-attributes response was observed.
func (p *Probe) DA1() bool { return p != nil && p.da1 }

func (p *Probe) scan(flush bool) []byte {
	var unrelated []byte
	for len(p.pending) != 0 {
		kittyStart := bytes.Index(p.pending, []byte("\x1b_G"))
		da1Start := bytes.Index(p.pending, []byte("\x1b["))
		start := -1
		if kittyStart >= 0 {
			start = kittyStart
		}
		if da1Start >= 0 && (start < 0 || da1Start < start) {
			start = da1Start
		}
		if start < 0 {
			if flush || len(p.pending) > 2 {
				keep := 0
				if !flush {
					keep = suffixPrefixLen(p.pending)
				}
				if len(p.pending) > keep {
					unrelated = append(unrelated, p.pending[:len(p.pending)-keep]...)
					p.pending = p.pending[len(p.pending)-keep:]
				}
			}
			break
		}
		if start > 0 {
			unrelated = append(unrelated, p.pending[:start]...)
			p.pending = p.pending[start:]
		}
		if p.pending[1] == '_' {
			end, complete := probeTerminator(p.pending)
			if !complete {
				if flush || len(p.pending) > maxProbeResponseBytes {
					unrelated = append(unrelated, p.pending[0])
					p.pending = p.pending[1:]
					continue
				}
				break
			}
			response := p.pending[:end]
			if validKittyResponse(response) {
				p.kitty = true
				p.pending = p.pending[end:]
				continue
			}
			unrelated = append(unrelated, p.pending[0])
			p.pending = p.pending[1:]
			continue
		}
		end, complete := da1Terminator(p.pending)
		if !complete {
			if flush || len(p.pending) > maxProbeResponseBytes {
				unrelated = append(unrelated, p.pending[0])
				p.pending = p.pending[1:]
				continue
			}
			break
		}
		response := p.pending[:end]
		if validDA1Response(response) {
			p.da1 = true
			p.pending = p.pending[end:]
			continue
		}
		unrelated = append(unrelated, p.pending[0])
		p.pending = p.pending[1:]
	}
	return unrelated
}

func probeTerminator(data []byte) (int, bool) {
	for i := 3; i+1 < len(data) && i <= maxProbeResponseBytes; i++ {
		if data[i] == '\x1b' && data[i+1] == '\\' {
			return i + 2, true
		}
	}
	return 0, false
}

func da1Terminator(data []byte) (int, bool) {
	for i := 2; i < len(data) && i <= maxProbeResponseBytes; i++ {
		if data[i] >= 0x40 && data[i] <= 0x7e {
			return i + 1, true
		}
	}
	return 0, false
}

func validKittyResponse(data []byte) bool {
	if len(data) < len("\x1b_Gi=31;OK\x1b\\") || !bytes.HasPrefix(data, []byte("\x1b_G")) || !bytes.HasSuffix(data, []byte("\x1b\\")) {
		return false
	}
	head := data[3 : len(data)-2]
	return bytes.Contains(head, []byte("i=31")) && bytes.Contains(head, []byte("OK"))
}

func validDA1Response(data []byte) bool {
	if len(data) < 4 || !bytes.HasPrefix(data, []byte("\x1b[")) || data[len(data)-1] != 'c' {
		return false
	}
	// DA1 is a primary response. Secondary DA (ESC[>...c) is deliberately
	// not sufficient for a direct graphics declaration.
	return data[2] == '?' && len(data) <= maxProbeResponseBytes
}

func suffixPrefixLen(data []byte) int {
	for n := min(len(data), 2); n > 0; n-- {
		if bytes.HasPrefix([]byte("\x1b_G"), data[len(data)-n:]) || bytes.HasPrefix([]byte("\x1b["), data[len(data)-n:]) {
			return n
		}
	}
	return 0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
