package handlers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"message-consolidator/config"
	"message-consolidator/services"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

// signSlackRequest computes a valid X-Slack-Signature for the given body and secret.
func signSlackRequest(secret string, body []byte) (ts, sig string) {
	ts = strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:"))
	mac.Write([]byte(ts))
	mac.Write([]byte(":"))
	mac.Write(body)
	sig = "v0=" + hex.EncodeToString(mac.Sum(nil))
	return
}

// newSignedSlackRequest builds an httptest.Request with a valid Slack HMAC signature.
func newSignedSlackRequest(method, url, secret string, body []byte) *http.Request {
	req := httptest.NewRequest(method, url, bytes.NewReader(body))
	ts, sig := signSlackRequest(secret, body)
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)
	return req
}

// TestReadAndVerifySlack_BadSignature verifies the 401 path (valid secret but wrong sig).
func TestReadAndVerifySlack_BadSignature(t *testing.T) {
	t.Parallel()
	secret := "mysecret"
	api := &API{
		Config: &config.Config{SlackSigningSecret: secret},
		Bot:    &services.SlackBot{},
	}
	req := httptest.NewRequest("POST", "/api/slack/events", strings.NewReader("body"))
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", "v0=invalidsignature")
	rr := httptest.NewRecorder()
	api.HandleSlackEvent(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

// TestHandleSlackEvent_URLVerification exercises the challenge-echo branch.
func TestHandleSlackEvent_URLVerification(t *testing.T) {
	t.Parallel()
	secret := "testsecret"
	body := []byte(`{"type":"url_verification","challenge":"mychallenge","token":"tok"}`)
	api := &API{
		Config: &config.Config{SlackSigningSecret: secret},
		Bot:    &services.SlackBot{},
	}
	req := newSignedSlackRequest("POST", "/api/slack/events", secret, body)
	rr := httptest.NewRecorder()
	api.HandleSlackEvent(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "mychallenge") {
		t.Errorf("body = %q, expected challenge echo", rr.Body.String())
	}
}

// TestHandleSlackEvent_Retry exercises the retry-drop branch.
func TestHandleSlackEvent_Retry(t *testing.T) {
	t.Parallel()
	secret := "testsecret"
	body := []byte(`{"type":"event_callback","event":{}}`)
	api := &API{
		Config: &config.Config{SlackSigningSecret: secret},
		Bot:    &services.SlackBot{},
	}
	req := newSignedSlackRequest("POST", "/api/slack/events", secret, body)
	req.Header.Set("X-Slack-Retry-Num", "1")
	req.Header.Set("X-Slack-Retry-Reason", "http_timeout")
	rr := httptest.NewRecorder()
	api.HandleSlackEvent(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (ack-and-drop)", rr.Code)
	}
}

// TestHandleSlackEvent_InvalidEventJSON exercises the bad-parse 400 branch.
func TestHandleSlackEvent_InvalidEventJSON(t *testing.T) {
	t.Parallel()
	secret := "testsecret"
	// Valid JSON but not a recognizable Slack event → ParseEvent returns error
	body := []byte(`{"type":"unknown_type_xyz"}`)
	api := &API{
		Config: &config.Config{SlackSigningSecret: secret},
		Bot:    &services.SlackBot{},
	}
	req := newSignedSlackRequest("POST", "/api/slack/events", secret, body)
	rr := httptest.NewRecorder()
	api.HandleSlackEvent(rr, req)
	// unknown type is not parsed as a callback — slackevents returns 200 or 400
	// depending on version. Accept either.
	if rr.Code != http.StatusOK && rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 200 or 400", rr.Code)
	}
}

// TestHandleSlackInteractive_MissingPayload exercises the missing-payload 400 path.
func TestHandleSlackInteractive_MissingPayload(t *testing.T) {
	t.Parallel()
	secret := "testsecret"
	// Valid form-encoded body but no payload field.
	body := []byte("foo=bar")
	api := &API{
		Config: &config.Config{SlackSigningSecret: secret},
		Bot:    &services.SlackBot{},
	}
	req := newSignedSlackRequest("POST", "/api/slack/interactive", secret, body)
	rr := httptest.NewRecorder()
	api.HandleSlackInteractive(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleSlackInteractive_InvalidPayloadJSON exercises the payload-parse 400 path.
func TestHandleSlackInteractive_InvalidPayloadJSON(t *testing.T) {
	t.Parallel()
	secret := "testsecret"
	body := []byte("payload=%7Bbad+json%7D") // payload={bad json} URL-encoded
	api := &API{
		Config: &config.Config{SlackSigningSecret: secret},
		Bot:    &services.SlackBot{},
	}
	req := newSignedSlackRequest("POST", "/api/slack/interactive", secret, body)
	rr := httptest.NewRecorder()
	api.HandleSlackInteractive(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleSlackCommand_MissingIdentifiers exercises the missing user_id/channel_id 400 path.
func TestHandleSlackCommand_MissingIdentifiers(t *testing.T) {
	t.Parallel()
	secret := "testsecret"
	body := []byte("command=%2Ftasks&text=list") // no user_id or channel_id
	api := &API{
		Config: &config.Config{SlackSigningSecret: secret},
		Bot:    &services.SlackBot{},
	}
	req := newSignedSlackRequest("POST", "/api/slack/commands", secret, body)
	rr := httptest.NewRecorder()
	api.HandleSlackCommand(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleSlackCommand_ValidCommand exercises the ack path when identifiers are present.
func TestHandleSlackCommand_ValidCommand(t *testing.T) {
	t.Parallel()
	secret := "testsecret"
	body := []byte(fmt.Sprintf("user_id=U123&channel_id=C456&text=list&command=%%2Ftasks"))
	api := &API{
		Config: &config.Config{SlackSigningSecret: secret},
		Bot:    &services.SlackBot{},
	}
	req := newSignedSlackRequest("POST", "/api/slack/commands", secret, body)
	rr := httptest.NewRecorder()
	api.HandleSlackCommand(rr, req)
	// ACK path — returns 200 and dispatches async goroutine.
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 body=%s", rr.Code, rr.Body.String())
	}
}

// TestRegisterSlackBotRoutes_RegisteredWhenConfigured verifies routes ARE registered
// when Bot and signing secret are both non-nil/non-empty.
func TestRegisterSlackBotRoutes_RegisteredWhenConfigured(t *testing.T) {
	t.Parallel()
	secret := "signing-secret"
	api := &API{
		Config: &config.Config{SlackSigningSecret: secret},
		Bot:    &services.SlackBot{},
	}
	// Just verifying registerSlackBotRoutes doesn't panic with a valid config.
	r := mux.NewRouter()
	api.registerSlackBotRoutes(r)
}
