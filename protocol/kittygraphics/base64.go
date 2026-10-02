package kittygraphics

import (
	"bytes"
	"encoding/base64"
	"fmt"
)

// DecodeBase64 accepts both padded standard base64 and unpadded raw standard
// base64. It deliberately does not accept URL-safe alphabets or non-base64
// whitespace other than the CR/LF ignored by the standard encoding.
//
// The returned slice is freshly allocated and owned by the caller.
func DecodeBase64(encoded []byte) ([]byte, error) {
	// RawStdEncoding rejects '=' and otherwise accepts every input
	// StdEncoding does (padded input has no '=' only when its length is a
	// multiple of four, where both encodings agree), so the alphabet used is
	// chosen by looking for padding rather than by decoding twice.
	enc := base64.RawStdEncoding
	if bytes.IndexByte(encoded, '=') >= 0 {
		enc = base64.StdEncoding
	}
	decoded := make([]byte, enc.DecodedLen(len(encoded)))
	n, err := enc.Decode(decoded, encoded)
	if err == nil {
		return decoded[:n], nil
	}
	if enc == base64.RawStdEncoding {
		// Report the StdEncoding error, as the padded decoder is the
		// documented primary format. This only runs for rejected input.
		_, err = base64.StdEncoding.Decode(make([]byte, base64.StdEncoding.DecodedLen(len(encoded))), encoded)
	}
	return nil, fmt.Errorf("%w: %v", ErrInvalidBase64, err)
}

// DecodePayload is a descriptive alias for DecodeBase64.
func DecodePayload(encoded []byte) ([]byte, error) { return DecodeBase64(encoded) }
