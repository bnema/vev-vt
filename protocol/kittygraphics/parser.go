package kittygraphics

import (
	"bytes"
	"fmt"
)

var apcPrefix = [...]byte{0x1b, '_', 'G'}

// Feed consumes data and returns text and complete graphics events. The
// parser retains only an incomplete APC or a possible split prefix between
// calls. A malformed APC is reported as an EventError and discarded through
// its string terminator, allowing following text and APCs to be processed.
func (p *Parser) Feed(data []byte) []Event {
	if p == nil {
		if len(data) == 0 {
			return nil
		}
		return []Event{{Kind: EventText, Text: append([]byte(nil), data...)}}
	}
	var events []Event
	text := make([]byte, 0, len(data))
	flushText := func() {
		if len(text) == 0 {
			return
		}
		events = append(events, Event{Kind: EventText, Text: append([]byte(nil), text...)})
		text = text[:0]
	}

	for _, b := range data {
		if !p.inAPC {
			p.candidate = append(p.candidate, b)
			for len(p.candidate) != 0 {
				if len(p.candidate) <= len(apcPrefix) && bytes.Equal(p.candidate, apcPrefix[:len(p.candidate)]) {
					break
				}
				text = append(text, p.candidate[0])
				p.candidate = p.candidate[1:]
			}
			if len(p.candidate) == len(apcPrefix) {
				flushText()
				p.candidate = p.candidate[:0]
				p.inAPC = true
				p.discard = false
				p.escaped = false
				p.oversize = false
				p.apc = p.apc[:0]
			}
			continue
		}

		if p.escaped {
			if b == '\\' {
				if p.discard || p.oversize {
					events = append(events, p.errorEvent())
				} else {
					events = append(events, p.commandEvent())
				}
				p.resetAPC()
				continue
			}
			// An ESC that is not followed by backslash is payload. Keep the
			// byte and continue looking for a terminator. This is necessary
			// for raw payloads and is harmless for base64 payloads.
			p.appendAPC(0x1b)
			p.escaped = false
		}
		if b == 0x9c { // C1 ST is also a valid APC terminator.
			if p.discard || p.oversize {
				events = append(events, p.errorEvent())
			} else {
				events = append(events, p.commandEvent())
			}
			p.resetAPC()
			continue
		}
		if b == 0x1b {
			p.escaped = true
			continue
		}
		p.appendAPC(b)
	}
	flushText()
	return events
}

// Parse is an alias for Feed.
func (p *Parser) Parse(data []byte) []Event { return p.Feed(data) }

// FeedWithErrors is a conventional error-returning facade over Feed. Parser
// errors are also retained in the returned Event values.
func (p *Parser) FeedWithErrors(data []byte) ([]Event, error) {
	events := p.Feed(data)
	for _, event := range events {
		if event.Kind == EventError && event.Err != nil {
			return events, event.Err
		}
	}
	return events, nil
}

// Finish reports an unterminated APC and resets the parser. Text which is a
// partial ordinary escape sequence is returned as text. Finish is useful at a
// stream boundary; a caller that will feed more data must not call it.
func (p *Parser) Finish() []Event {
	if p == nil {
		return nil
	}
	var events []Event
	if p.inAPC {
		err := ErrAPCTruncated
		if p.oversize {
			err = ErrAPCTooLarge
		}
		events = append(events, Event{Kind: EventError, Err: err})
		p.resetAPC()
	}
	if len(p.candidate) != 0 {
		events = append(events, Event{Kind: EventText, Text: append([]byte(nil), p.candidate...)})
		p.candidate = p.candidate[:0]
	}
	return events
}

// Flush is an alias for Finish.
func (p *Parser) Flush() []Event { return p.Finish() }

func (p *Parser) appendAPC(b byte) {
	if p.discard || p.oversize {
		return
	}
	if uint64(len(p.apc))+1 > p.limits.MaxAPCBytes {
		p.oversize = true
		return
	}
	p.apc = append(p.apc, b)
}

func (p *Parser) resetAPC() {
	p.inAPC = false
	p.discard = false
	p.escaped = false
	p.oversize = false
	p.apc = p.apc[:0]
}

func (p *Parser) errorEvent() Event {
	if p.oversize {
		return Event{Kind: EventError, Err: ErrAPCTooLarge}
	}
	return Event{Kind: EventError, Err: ErrInvalidAPC}
}

func (p *Parser) commandEvent() Event {
	command, err := ParseCommand(p.apc, p.limits)
	if err != nil {
		return Event{Kind: EventError, Err: err}
	}
	return Event{Kind: EventCommand, Command: command}
}

// ParseCommand parses the bytes between APC's G and its ST terminator. It is
// exported separately so adapters can validate commands without maintaining a
// stream parser.
func ParseCommand(body []byte, config ...Limits) (Command, error) {
	var l Limits
	if len(config) != 0 {
		l = config[0]
	}
	l = normalizeLimits(l)
	if uint64(len(body)) > l.MaxAPCBytes {
		return Command{}, ErrAPCTooLarge
	}
	sep := bytes.IndexByte(body, ';')
	if sep < 0 {
		return Command{}, fmt.Errorf("%w: missing payload separator", ErrInvalidCommand)
	}
	controls, err := parseControls(body[:sep])
	if err != nil {
		return Command{}, err
	}
	payload := body[sep+1:]
	if uint64(len(payload)) > l.MaxPayloadBytes {
		return Command{}, ErrPayloadTooLarge
	}
	return Command{Controls: controls, Payload: append([]byte(nil), payload...)}, nil
}

// ParseAPC parses a complete ESC _ G ... ST sequence.
func ParseAPC(apc []byte, config ...Limits) (Command, error) {
	if len(apc) < 5 || apc[0] != 0x1b || apc[1] != '_' || apc[2] != 'G' {
		return Command{}, ErrInvalidAPC
	}
	body := apc[3:]
	if body[len(body)-1] == 0x9c {
		body = body[:len(body)-1]
	} else if len(body) >= 2 && body[len(body)-2] == 0x1b && body[len(body)-1] == '\\' {
		body = body[:len(body)-2]
	} else {
		return Command{}, ErrAPCTruncated
	}
	return ParseCommand(body, config...)
}
