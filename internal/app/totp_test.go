package app

import (
	"bytes"
	"encoding/base32"
	"testing"
)

func TestTOTPRFC6238AndEncryption(t *testing.T) {
	// RFC 6238 appendix B SHA-1 seed.
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	if got := totpAt(secret, 59/30); got != "287082" {
		t.Fatalf("RFC TOTP vector %s", got)
	}
	key := bytes.Repeat([]byte{42}, 32)
	encrypted, e := sealSecret(key, secret)
	if e != nil {
		t.Fatal(e)
	}
	plain, e := openSecret(key, encrypted)
	if e != nil || plain != secret {
		t.Fatal("encrypted secret round trip")
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, e = openSecret(key, encrypted); e == nil {
		t.Fatal("tampered secret decrypted")
	}
}
