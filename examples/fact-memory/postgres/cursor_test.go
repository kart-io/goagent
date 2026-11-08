package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/kart-io/goagent/memory"
)

func TestSignedCursorRejectsTamperingBindingAndExpiry(t *testing.T) {
	secret := []byte("test-cursor-signing-secret")
	now := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	cursor := listCursor{Version: cursorVersion, Kind: "facts", Binding: "query-binding", Cutoff: 42, Updated: now, ID: "fact-1", Expires: now.Add(time.Minute).Unix()}
	token, err := encodeCursor(secret, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeCursor(secret, token, "query-binding", now); err != nil {
		t.Fatalf("fresh cursor rejected: %v", err)
	}
	if _, err = decodeCursor(secret, token, "another-query", now); !errors.Is(err, memory.ErrInvalidCursor) {
		t.Fatalf("cross-query cursor accepted: %v", err)
	}
	if _, err = decodeCursor(secret, token+"x", "query-binding", now); !errors.Is(err, memory.ErrInvalidCursor) {
		t.Fatalf("tampered cursor accepted: %v", err)
	}
	if _, err = decodeCursor(secret, token, "query-binding", now.Add(2*time.Minute)); !errors.Is(err, memory.ErrInvalidCursor) {
		t.Fatalf("expired cursor accepted: %v", err)
	}
}
