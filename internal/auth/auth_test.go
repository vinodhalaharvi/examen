package auth

import (
	"strings"
	"testing"
	"time"
)

func newTestConfig(t *testing.T) *Config {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return &Config{CookieKey: key, Insecure: true}
}

func TestEncodeDecode_RoundTrip(t *testing.T) {
	c := newTestConfig(t)
	now := time.Now().Truncate(time.Second)
	want := Session{
		StudentID: "alice@example.com",
		Email:     "alice@example.com",
		Name:      "Alice Example",
		Picture:   "https://example.com/avatar.png",
		IssuedAt:  now,
		ExpiresAt: now.Add(time.Hour),
	}
	encoded := c.Encode(want)
	got, err := c.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.StudentID != want.StudentID || got.Email != want.Email || got.Name != want.Name {
		t.Errorf("round-trip mismatch:\n got:  %+v\n want: %+v", got, want)
	}
}

func TestDecode_ExpiredSessionRejected(t *testing.T) {
	c := newTestConfig(t)
	old := Session{
		StudentID: "alice@example.com",
		IssuedAt:  time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	encoded := c.Encode(old)
	if _, err := c.Decode(encoded); err == nil {
		t.Fatal("expected expired session to be rejected")
	}
}

func TestDecode_TamperedSignatureRejected(t *testing.T) {
	c := newTestConfig(t)
	s := Session{
		StudentID: "alice@example.com",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	encoded := c.Encode(s)
	parts := strings.SplitN(encoded, ".", 2)
	if len(parts) != 2 {
		t.Fatal("malformed encoded value")
	}
	// Flip a byte in the signature
	bad := parts[0] + "." + flipFirstChar(parts[1])
	if _, err := c.Decode(bad); err == nil {
		t.Fatal("tampered signature should be rejected")
	}
}

func TestDecode_TamperedPayloadRejected(t *testing.T) {
	c := newTestConfig(t)
	s := Session{
		StudentID: "alice@example.com",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	encoded := c.Encode(s)
	parts := strings.SplitN(encoded, ".", 2)
	bad := flipFirstChar(parts[0]) + "." + parts[1]
	if _, err := c.Decode(bad); err == nil {
		t.Fatal("tampered payload should be rejected")
	}
}

func TestDecode_DifferentKeyRejected(t *testing.T) {
	c1 := newTestConfig(t)
	c2 := &Config{CookieKey: []byte("a-completely-different-key-1234"), Insecure: true}
	s := Session{
		StudentID: "alice@example.com",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	encoded := c1.Encode(s)
	if _, err := c2.Decode(encoded); err == nil {
		t.Fatal("cookie signed with different key should be rejected")
	}
}

func TestDecode_MalformedCookie(t *testing.T) {
	c := newTestConfig(t)
	for _, bad := range []string{"", "no-dot", "too.many.dots", "...", "x.y.z"} {
		if _, err := c.Decode(bad); err == nil {
			t.Errorf("malformed cookie %q should be rejected", bad)
		}
	}
}

// flipFirstChar replaces the first character with a different one, for tamper tests.
func flipFirstChar(s string) string {
	if s == "" {
		return "x"
	}
	if s[0] == 'a' {
		return "b" + s[1:]
	}
	return "a" + s[1:]
}
