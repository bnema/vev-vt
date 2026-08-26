package kittygraphics

import (
	"encoding/base64"
	"fmt"
)

// DecodeBase64 accepts both padded standard base64 and unpadded raw standard
// base64. It deliberately does not accept URL-safe alphabets or non-base64
// whitespace other than the CR/LF ignored by the standard encoding.
func DecodeBase64(encoded []byte) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(string(encoded))
	if err == nil {
		return decoded, nil
	}
	decoded, rawErr := base64.RawStdEncoding.DecodeString(string(encoded))
	if rawErr == nil {
		return decoded, nil
	}
	return nil, fmt.Errorf("%w: %v", ErrInvalidBase64, err)
}

// DecodePayload is a descriptive alias for DecodeBase64.
func DecodePayload(encoded []byte) ([]byte, error) { return DecodeBase64(encoded) }
