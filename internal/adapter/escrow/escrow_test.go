package escrow

import (
	"errors"
	"testing"
	"time"
)

func TestSignatureRoundTripAndReplayWindow(t *testing.T) {
	secret := []byte("s3cr3t")
	body := []byte(`{"type":"escrow.held"}`)
	now := time.Unix(1_800_000_000, 0)
	h := Sign(secret, now, body)
	if err := Verify(secret, h, body, now.Add(time.Minute), 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := Verify(secret, h, []byte(`{"type":"escrow.released"}`), now, 5*time.Minute); !errors.Is(err, ErrSignature) {
		t.Fatal("tampered body accepted")
	}
	if err := Verify([]byte("other"), h, body, now, 5*time.Minute); !errors.Is(err, ErrSignature) {
		t.Fatal("wrong secret accepted")
	}
	if err := Verify(secret, h, body, now.Add(10*time.Minute), 5*time.Minute); !errors.Is(err, ErrSignature) {
		t.Fatal("replay outside the window accepted")
	}
	if err := Verify(secret, "garbage", body, now, time.Minute); !errors.Is(err, ErrSignature) {
		t.Fatal("garbage header accepted")
	}
}
