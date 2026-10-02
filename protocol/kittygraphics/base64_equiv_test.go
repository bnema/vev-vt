package kittygraphics

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"math/rand"
	"testing"
)

// legacyDecodeBase64 is the original two-pass implementation, kept as the
// accept/reject and error-text oracle for DecodeBase64.
func legacyDecodeBase64(encoded []byte) ([]byte, error) {
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

func checkBase64Equivalent(t testing.TB, input []byte) {
	t.Helper()
	want, wantErr := legacyDecodeBase64(input)
	got, gotErr := DecodeBase64(input)
	if (wantErr == nil) != (gotErr == nil) {
		t.Fatalf("DecodeBase64(%q) error = %v, legacy error = %v", input, gotErr, wantErr)
	}
	if wantErr != nil {
		if gotErr.Error() != wantErr.Error() {
			t.Fatalf("DecodeBase64(%q) error = %q, legacy %q", input, gotErr, wantErr)
		}
		if !errorsIs(gotErr, ErrInvalidBase64) {
			t.Fatalf("DecodeBase64(%q) error %v does not wrap ErrInvalidBase64", input, gotErr)
		}
		return
	}
	if !bytes.Equal(got, want) || (got == nil) != (want == nil) {
		t.Fatalf("DecodeBase64(%q) = %x, legacy %x", input, got, want)
	}
}

func TestDecodeBase64MatchesLegacyRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	alphabets := []string{
		"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/",
		"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=\r\n",
		"AAAA==\r\n=-_ \t\x00\xff",
	}
	for i := 0; i < 200000; i++ {
		alphabet := alphabets[rng.Intn(len(alphabets))]
		input := make([]byte, rng.Intn(24))
		for j := range input {
			input[j] = alphabet[rng.Intn(len(alphabet))]
		}
		checkBase64Equivalent(t, input)
	}
	// Valid encodings, padded and raw, with and without line breaks.
	for n := 0; n < 40; n++ {
		raw := make([]byte, n)
		rng.Read(raw)
		padded := base64.StdEncoding.EncodeToString(raw)
		unpadded := base64.RawStdEncoding.EncodeToString(raw)
		checkBase64Equivalent(t, []byte(padded))
		checkBase64Equivalent(t, []byte(unpadded))
		checkBase64Equivalent(t, []byte(unpadded+"\r\n"))
		if len(padded) > 2 {
			checkBase64Equivalent(t, []byte(padded[:2]+"\n"+padded[2:]))
			checkBase64Equivalent(t, []byte(padded[:len(padded)-1]))
		}
	}
	checkBase64Equivalent(t, nil)
	checkBase64Equivalent(t, []byte{})
}

func FuzzDecodeBase64MatchesLegacy(f *testing.F) {
	for _, seed := range []string{"", "AAAA", "AAA=", "AAA", "AA==", "AA", "A", "AA=", "=", "AAAA\n", "AA\r\nAA", "-_-_"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) { checkBase64Equivalent(t, input) })
}

func TestParseAPCBorrowedAliasesAndParseAPCCopies(t *testing.T) {
	apc := []byte("\x1b_Ga=t,i=1;QUJD\x1b\\")
	copied, err := ParseAPC(apc)
	if err != nil {
		t.Fatal(err)
	}
	borrowed, err := ParseAPCBorrowed(apc)
	if err != nil {
		t.Fatal(err)
	}
	apc[len("\x1b_Ga=t,i=1;")] = 'Z'
	if string(copied.Payload) != "QUJD" {
		t.Fatalf("ParseAPC payload aliased input: %q", copied.Payload)
	}
	if string(borrowed.Payload) != "ZUJD" {
		t.Fatalf("ParseAPCBorrowed payload = %q, want aliased", borrowed.Payload)
	}
}
