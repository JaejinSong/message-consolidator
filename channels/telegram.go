// Package channels — Telegram MTProto user-level client (Phase A: auth skeleton).
// Manages per-user sessions via gotd/td, bridges the HTTP 3-step phone/OTP/2FA flow
// and the blocking client.Run goroutine through per-user channels.
//
// Message ingestion (update handlers, PopMessages) is deferred to Phase B.
package channels

import (
	"context"
	"errors"
	"fmt"
	"message-consolidator/config"
	"message-consolidator/logger"
	"sync"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
)

const (
	TGStatusDisconnected    = "disconnected"
	TGStatusPendingCode     = "pending_code"
	TGStatusPendingPassword = "pending_password"
	TGStatusConnected       = "connected"
)

const (
	tgAuthStartTimeout   = 30 * time.Second
	tgAuthConfirmTimeout = 20 * time.Second
	tgChannelSendTimeout = 2 * time.Second
)

// tgAuthState is the per-user bridge between HTTP handlers and the client.Run goroutine.
// Protected by its own mutex to keep callbacks from the gotd goroutine race-free
// with handler reads of status/userID.
type tgAuthState struct {
	phone    string
	codeChan chan string
	passChan chan string
	doneChan chan error
	cancel   context.CancelFunc

	mu     sync.RWMutex
	status string
	userID int64
}

func (s *tgAuthState) setStatus(st string) {
	s.mu.Lock()
	s.status = st
	s.mu.Unlock()
}

func (s *tgAuthState) getStatus() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// TelegramManager owns all per-user Telegram client goroutines and their auth state.
// Callbacks (FetchUserTgSession / OnSessionUpdated / OnConnected / OnLoggedOut) implement
// the same IoC pattern used by WAManager — they decouple this package from store/.
type TelegramManager struct {
	states map[string]*tgAuthState
	mu     sync.RWMutex

	FetchUserTgSession func(email string) ([]byte, error)
	OnSessionUpdated   func(email string, data []byte)
	OnConnected        func(email string, userID int64)
	OnLoggedOut        func(email string)
}

// DefaultTelegramManager mirrors DefaultWAManager — process-wide singleton.
var DefaultTelegramManager = NewTelegramManager()

func NewTelegramManager() *TelegramManager {
	return &TelegramManager{
		states:             make(map[string]*tgAuthState),
		FetchUserTgSession: func(email string) ([]byte, error) { return nil, nil },
		OnSessionUpdated:   func(email string, data []byte) {},
		OnConnected:        func(email string, userID int64) {},
		OnLoggedOut:        func(email string) {},
	}
}

// dbSessionStorage satisfies session.Storage by delegating to the manager's IoC callbacks.
// An empty/missing session is reported via session.ErrNotFound, which gotd treats as
// "no session yet, start auth flow".
type dbSessionStorage struct {
	email   string
	manager *TelegramManager
}

func (s *dbSessionStorage) LoadSession(_ context.Context) ([]byte, error) {
	data, err := s.manager.FetchUserTgSession(s.email)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, session.ErrNotFound
	}
	return data, nil
}

func (s *dbSessionStorage) StoreSession(_ context.Context, data []byte) error {
	s.manager.OnSessionUpdated(s.email, data)
	return nil
}

// channelAuth satisfies auth.UserAuthenticator by reading phone/code/password from
// per-user channels fed by the HTTP handlers. Status transitions are published before
// each blocking read so callers can poll GetStatus.
type channelAuth struct {
	state *tgAuthState
}

func (c *channelAuth) Phone(_ context.Context) (string, error) {
	return c.state.phone, nil
}

func (c *channelAuth) Password(ctx context.Context) (string, error) {
	c.state.setStatus(TGStatusPendingPassword)
	select {
	case p := <-c.state.passChan:
		return p, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (c *channelAuth) Code(ctx context.Context, _ *tg.AuthSentCode) (string, error) {
	c.state.setStatus(TGStatusPendingCode)
	select {
	case code := <-c.state.codeChan:
		return code, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (c *channelAuth) AcceptTermsOfService(_ context.Context, _ tg.HelpTermsOfService) error {
	return nil
}

func (c *channelAuth) SignUp(_ context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("telegram: sign-up not supported")
}

// InitTelegram attempts to restore a session for the given user at startup.
// No session → silently returns; manual /api/telegram/auth/start is required.
func (m *TelegramManager) InitTelegram(email string, cfg *config.Config) {
	if cfg.TelegramAppID == 0 || cfg.TelegramAppHash == "" {
		return
	}
	data, err := m.FetchUserTgSession(email)
	if err != nil || len(data) == 0 {
		return
	}
	if err := m.startClient(email, cfg, "", true); err != nil {
		logger.Warnf("[TG] session restore failed for %s: %v", email, err)
	}
}

// StartAuth begins a fresh phone-number auth. Cancels any prior attempt for the email.
// Blocks until the auth goroutine reports pending_code (SendCode completed) or fails.
func (m *TelegramManager) StartAuth(email, phone string, cfg *config.Config) error {
	if cfg.TelegramAppID == 0 || cfg.TelegramAppHash == "" {
		return errors.New("telegram: TELEGRAM_APP_ID/HASH not configured")
	}
	if phone == "" {
		return errors.New("telegram: phone required")
	}

	m.cancelPrevious(email)

	if err := m.startClient(email, cfg, phone, false); err != nil {
		return err
	}
	return m.waitForStatus(email, tgAuthStartTimeout, TGStatusPendingCode, TGStatusConnected)
}

func (m *TelegramManager) cancelPrevious(email string) {
	m.mu.Lock()
	s, ok := m.states[email]
	if ok {
		delete(m.states, email)
	}
	m.mu.Unlock()
	if ok && s.cancel != nil {
		s.cancel()
	}
}

// ConfirmCode forwards the OTP and waits for either connected or pending_password.
// Returns (needsPassword, error) so the HTTP layer can branch to the 2FA step.
func (m *TelegramManager) ConfirmCode(email, code string) (bool, error) {
	s, err := m.lookupState(email)
	if err != nil {
		return false, err
	}
	if err := sendOrTimeout(s.codeChan, code); err != nil {
		return false, err
	}
	deadline := time.After(tgAuthConfirmTimeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			return false, errors.New("telegram: SignIn timeout")
		case err := <-s.doneChan:
			if err != nil {
				return false, err
			}
			return false, nil
		case <-ticker.C:
			switch s.getStatus() {
			case TGStatusConnected:
				return false, nil
			case TGStatusPendingPassword:
				return true, nil
			}
		}
	}
}

// ConfirmPassword forwards the 2FA password and waits for the connected state.
func (m *TelegramManager) ConfirmPassword(email, password string) error {
	s, err := m.lookupState(email)
	if err != nil {
		return err
	}
	if err := sendOrTimeout(s.passChan, password); err != nil {
		return err
	}
	return m.waitForStatus(email, tgAuthConfirmTimeout, TGStatusConnected)
}

func (m *TelegramManager) lookupState(email string) (*tgAuthState, error) {
	m.mu.RLock()
	s, ok := m.states[email]
	m.mu.RUnlock()
	if !ok {
		return nil, errors.New("telegram: no pending auth")
	}
	return s, nil
}

func sendOrTimeout(ch chan string, v string) error {
	select {
	case ch <- v:
		return nil
	case <-time.After(tgChannelSendTimeout):
		return errors.New("telegram: auth channel not ready")
	}
}

// waitForStatus polls the state's status until one of the terminal values is observed
// or the timeout elapses. On doneChan receive, surfaces the auth-goroutine error.
func (m *TelegramManager) waitForStatus(email string, timeout time.Duration, terminal ...string) error {
	s, err := m.lookupState(email)
	if err != nil {
		return err
	}
	deadline := time.After(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			return fmt.Errorf("telegram: wait for %v timed out", terminal)
		case err := <-s.doneChan:
			if err != nil {
				return err
			}
			if s.getStatus() == TGStatusConnected {
				return nil
			}
			return errors.New("telegram: auth goroutine exited without connection")
		case <-ticker.C:
			st := s.getStatus()
			for _, t := range terminal {
				if st == t {
					return nil
				}
			}
		}
	}
}

// GetStatus reports the current auth/connection status for the user, or
// TGStatusDisconnected when no goroutine is tracked.
func (m *TelegramManager) GetStatus(email string) string {
	m.mu.RLock()
	s, ok := m.states[email]
	m.mu.RUnlock()
	if !ok {
		return TGStatusDisconnected
	}
	return s.getStatus()
}

// LogoutTelegram cancels the client goroutine and fires the OnLoggedOut callback.
// Actual session data deletion is delegated to the store layer via that callback.
func (m *TelegramManager) LogoutTelegram(email string) error {
	m.mu.Lock()
	s, ok := m.states[email]
	if ok {
		delete(m.states, email)
	}
	m.mu.Unlock()
	if ok && s.cancel != nil {
		s.cancel()
	}
	m.OnLoggedOut(email)
	return nil
}

func (m *TelegramManager) startClient(email string, cfg *config.Config, phone string, restoreOnly bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	state := &tgAuthState{
		phone:    phone,
		status:   TGStatusDisconnected,
		codeChan: make(chan string, 1),
		passChan: make(chan string, 1),
		doneChan: make(chan error, 1),
		cancel:   cancel,
	}
	m.mu.Lock()
	m.states[email] = state
	m.mu.Unlock()

	storage := &dbSessionStorage{email: email, manager: m}
	client := telegram.NewClient(cfg.TelegramAppID, cfg.TelegramAppHash, telegram.Options{
		SessionStorage: storage,
	})

	go m.runClient(ctx, email, client, state, restoreOnly)
	return nil
}

func (m *TelegramManager) runClient(ctx context.Context, email string, client *telegram.Client, state *tgAuthState, restoreOnly bool) {
	defer func() {
		// Only clear to disconnected if we never reached connected; a deliberate Logout
		// already removed the state from the map, so this is a no-op in that path.
		if state.getStatus() != TGStatusConnected {
			state.setStatus(TGStatusDisconnected)
		}
	}()

	err := client.Run(ctx, func(ctx context.Context) error {
		if err := m.ensureAuthorized(ctx, client, state, restoreOnly); err != nil {
			return err
		}

		self, err := client.Self(ctx)
		if err != nil {
			return fmt.Errorf("self: %w", err)
		}
		state.mu.Lock()
		state.userID = self.ID
		state.status = TGStatusConnected
		state.mu.Unlock()
		m.OnConnected(email, self.ID)
		logger.Infof("[TG] connected for %s (userID=%d)", email, self.ID)

		// Phase A keeps the client alive so the session stays valid; Phase B will
		// attach an UpdateDispatcher for inbound message buffering here.
		<-ctx.Done()
		return nil
	})

	// Non-blocking done signal — the most recent confirm-step is the only expected reader.
	select {
	case state.doneChan <- err:
	default:
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		logger.Warnf("[TG] client.Run exit for %s: %v", email, err)
	}
}

func (m *TelegramManager) ensureAuthorized(ctx context.Context, client *telegram.Client, state *tgAuthState, restoreOnly bool) error {
	if restoreOnly {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("auth status: %w", err)
		}
		if !status.Authorized {
			return errors.New("session present but not authorized")
		}
		return nil
	}
	flow := auth.NewFlow(&channelAuth{state: state}, auth.SendCodeOptions{})
	if err := client.Auth().IfNecessary(ctx, flow); err != nil {
		return fmt.Errorf("auth flow: %w", err)
	}
	return nil
}

// DisconnectAllTelegram cancels every tracked client goroutine. Used during shutdown.
func DisconnectAllTelegram() { DefaultTelegramManager.DisconnectAll() }

func (m *TelegramManager) DisconnectAll() {
	m.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(m.states))
	for _, s := range m.states {
		if s.cancel != nil {
			cancels = append(cancels, s.cancel)
		}
	}
	m.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// Package-level accessors — mirror the WhatsApp convention (GetWhatsAppStatus, etc.).

func GetTelegramStatus(email string) string { return DefaultTelegramManager.GetStatus(email) }

func StartTelegramAuth(email, phone string, cfg *config.Config) error {
	return DefaultTelegramManager.StartAuth(email, phone, cfg)
}

func ConfirmTelegramCode(email, code string) (bool, error) {
	return DefaultTelegramManager.ConfirmCode(email, code)
}

func ConfirmTelegramPassword(email, password string) error {
	return DefaultTelegramManager.ConfirmPassword(email, password)
}

func LogoutTelegram(email string) error { return DefaultTelegramManager.LogoutTelegram(email) }
