package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
	"time"
)

func signSlack(t *testing.T, secret, ts string, body []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + ts + ":"))
	mac.Write(body)
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySlackRequest_Positive(t *testing.T) {
	secret := "shhh"
	body := []byte(`{"type":"event_callback"}`)
	now := time.Unix(1_700_000_000, 0)
	ts := strconv.FormatInt(now.Unix(), 10)
	sig := signSlack(t, secret, ts, body)

	if err := verifySlackRequestAt(secret, ts, sig, body, now); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestVerifySlackRequest_ExpiredTimestamp(t *testing.T) {
	secret := "shhh"
	body := []byte("{}")
	now := time.Unix(1_700_000_000, 0)
	old := now.Add(-10 * time.Minute)
	ts := strconv.FormatInt(old.Unix(), 10)
	sig := signSlack(t, secret, ts, body)

	err := verifySlackRequestAt(secret, ts, sig, body, now)
	if !errors.Is(err, ErrSlackTimestampExpired) {
		t.Fatalf("expected ErrSlackTimestampExpired, got %v", err)
	}
}

func TestVerifySlackRequest_BadSignature(t *testing.T) {
	secret := "shhh"
	body := []byte("{}")
	now := time.Unix(1_700_000_000, 0)
	ts := strconv.FormatInt(now.Unix(), 10)

	err := verifySlackRequestAt(secret, ts, "v0=deadbeef", body, now)
	if !errors.Is(err, ErrSlackSignatureMismatch) {
		t.Fatalf("expected ErrSlackSignatureMismatch, got %v", err)
	}
}

func TestVerifySlackRequest_MissingSecret(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	ts := strconv.FormatInt(now.Unix(), 10)
	if err := verifySlackRequestAt("", ts, "v0=x", []byte("{}"), now); !errors.Is(err, ErrSlackSecretMissing) {
		t.Fatalf("expected ErrSlackSecretMissing, got %v", err)
	}
}

func TestVerifySlackRequest_BodyTamper(t *testing.T) {
	secret := "shhh"
	body := []byte(`{"a":1}`)
	now := time.Unix(1_700_000_000, 0)
	ts := strconv.FormatInt(now.Unix(), 10)
	sig := signSlack(t, secret, ts, body)

	tampered := []byte(`{"a":2}`)
	if err := verifySlackRequestAt(secret, ts, sig, tampered, now); !errors.Is(err, ErrSlackSignatureMismatch) {
		t.Fatalf("expected ErrSlackSignatureMismatch, got %v", err)
	}
}

func TestVerifySlackRequest_MissingSignature(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	ts := strconv.FormatInt(now.Unix(), 10)
	if err := verifySlackRequestAt("shhh", ts, "", []byte("{}"), now); !errors.Is(err, ErrSlackSignatureMissing) {
		t.Fatalf("expected ErrSlackSignatureMissing, got %v", err)
	}
}

func TestVerifySlackRequest_MissingTimestamp(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	if err := verifySlackRequestAt("shhh", "", "v0=x", []byte("{}"), now); !errors.Is(err, ErrSlackTimestampMissing) {
		t.Fatalf("expected ErrSlackTimestampMissing, got %v", err)
	}
}

func TestVerifySlackRequest_InvalidTimestamp(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	err := verifySlackRequestAt("shhh", "not-a-number", "v0=x", []byte("{}"), now)
	if !errors.Is(err, ErrSlackTimestampInvalid) {
		t.Fatalf("expected ErrSlackTimestampInvalid, got %v", err)
	}
}

func TestVerifySlackRequest_PublicWrapperUsesNow(t *testing.T) {
	// Why: Exercises the exported wrapper so a future signing-secret rotation
	// regression is caught at the public surface, not just the internal helper.
	secret := "shhh"
	body := []byte("{}")
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signSlack(t, secret, ts, body)
	if err := VerifySlackRequest(secret, ts, sig, body); err != nil {
		t.Fatalf("VerifySlackRequest with current ts should pass, got %v", err)
	}
}

func TestVerifySlackRequest_FutureTimestampWithinWindow(t *testing.T) {
	secret := "shhh"
	body := []byte("{}")
	now := time.Unix(1_700_000_000, 0)
	future := now.Add(2 * time.Minute)
	ts := strconv.FormatInt(future.Unix(), 10)
	sig := signSlack(t, secret, ts, body)

	if err := verifySlackRequestAt(secret, ts, sig, body, now); err != nil {
		t.Fatalf("future ts within window must accept, got %v", err)
	}
}
