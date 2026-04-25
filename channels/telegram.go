// Package channels — Telegram MTProto user-level client.
// Manages per-user sessions via gotd/td, bridges the HTTP 3-step phone/OTP/2FA flow
// and the blocking client.Run goroutine through per-user channels, and buffers
// inbound messages for the scanner via a tg.UpdateDispatcher.
package channels

import (
	"context"
	"errors"
	"fmt"
	"message-consolidator/config"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"strconv"
	"strings"
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
	states        map[string]*tgAuthState
	messageBuffer map[string]map[string][]types.RawMessage
	clients       map[string]*telegram.Client
	mu            sync.RWMutex

	// groupCache: chatKey ("tg_user_X" / "tg_chat_X" / "tg_channel_X") → display title.
	// Populated from tg.Entities on every ingest and from MessagesGetDialogs at connect.
	// Why: group/channel titles are only reachable via Telegram API; persisting them
	// to contacts overloads the table, so an in-memory cache mirrors WhatsApp's pattern.
	groupCache sync.Map

	FetchUserTgSession func(email string) ([]byte, error)
	FetchUserTgCreds   func(email string) (appID int, appHash string, ok bool)
	OnSessionUpdated   func(email string, data []byte)
	OnConnected        func(email string, userID int64)
	OnLoggedOut        func(email string)
}

// TelegramCreds is the resolved App ID/Hash pair used to instantiate a gotd client.
type TelegramCreds struct {
	AppID   int
	AppHash string
}

// DefaultTelegramManager mirrors DefaultWAManager — process-wide singleton.
var DefaultTelegramManager = NewTelegramManager()

func NewTelegramManager() *TelegramManager {
	return &TelegramManager{
		states:             make(map[string]*tgAuthState),
		messageBuffer:      make(map[string]map[string][]types.RawMessage),
		clients:            make(map[string]*telegram.Client),
		FetchUserTgSession: func(email string) ([]byte, error) { return nil, nil },
		FetchUserTgCreds:   func(email string) (int, string, bool) { return 0, "", false },
		OnSessionUpdated:   func(email string, data []byte) {},
		OnConnected:        func(email string, userID int64) {},
		OnLoggedOut:        func(email string) {},
	}
}

// resolveCreds prefers per-user DB credentials (entered via the UI) and falls back
// to the process-wide env-configured pair. Returns an error only when neither source
// has a complete (non-zero AppID + non-empty AppHash) pair.
func (m *TelegramManager) resolveCreds(email string, cfg *config.Config) (TelegramCreds, error) {
	if m.FetchUserTgCreds != nil {
		if id, hash, ok := m.FetchUserTgCreds(email); ok && id != 0 && hash != "" {
			return TelegramCreds{AppID: id, AppHash: hash}, nil
		}
	}
	if cfg != nil && cfg.TelegramAppID != 0 && cfg.TelegramAppHash != "" {
		return TelegramCreds{AppID: cfg.TelegramAppID, AppHash: cfg.TelegramAppHash}, nil
	}
	return TelegramCreds{}, errors.New("telegram: credentials not configured — set them via the UI or TELEGRAM_APP_ID/HASH env")
}

// HasCredentials reports whether StartAuth would find a usable App ID/Hash pair.
// Used by the status handler so the UI can show the credentials step proactively.
func (m *TelegramManager) HasCredentials(email string, cfg *config.Config) bool {
	_, err := m.resolveCreds(email, cfg)
	return err == nil
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
// No session or no credentials → silently returns; manual /api/telegram/auth/start is required.
func (m *TelegramManager) InitTelegram(email string, cfg *config.Config) {
	creds, err := m.resolveCreds(email, cfg)
	if err != nil {
		return
	}
	data, err := m.FetchUserTgSession(email)
	if err != nil || len(data) == 0 {
		return
	}
	if err := m.startClient(email, creds, "", true); err != nil {
		logger.Warnf("[TG] session restore failed for %s: %v", email, err)
	}
}

// StartAuth begins a fresh phone-number auth. Cancels any prior attempt for the email.
// Blocks until the auth goroutine reports pending_code (SendCode completed) or fails.
func (m *TelegramManager) StartAuth(email, phone string, cfg *config.Config) error {
	if phone == "" {
		return errors.New("telegram: phone required")
	}
	creds, err := m.resolveCreds(email, cfg)
	if err != nil {
		return err
	}

	m.cancelPrevious(email)

	if err := m.startClient(email, creds, phone, false); err != nil {
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

func (m *TelegramManager) startClient(email string, creds TelegramCreds, phone string, restoreOnly bool) error {
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
	dispatcher := m.newDispatcher(email)
	client := telegram.NewClient(creds.AppID, creds.AppHash, telegram.Options{
		SessionStorage: storage,
		UpdateHandler:  dispatcher,
	})

	m.mu.Lock()
	m.clients[email] = client
	m.mu.Unlock()

	go m.runClient(ctx, email, client, state, restoreOnly)
	return nil
}

func (m *TelegramManager) runClient(ctx context.Context, email string, client *telegram.Client, state *tgAuthState, restoreOnly bool) {
	defer func() {
		if state.getStatus() != TGStatusConnected {
			state.setStatus(TGStatusDisconnected)
		}
		m.dropBuffer(email)
		m.mu.Lock()
		delete(m.clients, email)
		m.mu.Unlock()
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

		// Hydrate groupCache + contacts from the dialog list so GetGroupName can
		// resolve titles for dormant chats that haven't pushed a new message yet.
		go m.hydrateDialogs(ctx, client, email)

		// Block until ctx is cancelled — inbound updates are delivered via the
		// UpdateDispatcher registered on telegram.Options.UpdateHandler.
		<-ctx.Done()
		return nil
	})

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

func HasTelegramCredentials(email string, cfg *config.Config) bool {
	return DefaultTelegramManager.HasCredentials(email, cfg)
}

// newDispatcher builds the per-user UpdateDispatcher. Registered in startClient
// via telegram.Options.UpdateHandler so the gotd client invokes it on every push.
func (m *TelegramManager) newDispatcher(email string) tg.UpdateDispatcher {
	d := tg.NewUpdateDispatcher()
	d.OnNewMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
		m.ingestMessage(email, e, u.Message)
		return nil
	})
	d.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
		m.ingestMessage(email, e, u.Message)
		return nil
	})
	return d
}

// ingestMessage narrows MessageClass to *tg.Message (skips MessageService/Empty)
// and pushes a normalized RawMessage into the per-chat buffer.
func (m *TelegramManager) ingestMessage(email string, e tg.Entities, mc tg.MessageClass) {
	msg, ok := mc.(*tg.Message)
	if !ok || msg.Message == "" {
		return
	}
	chatKey, ok := peerKey(msg.PeerID)
	if !ok {
		return
	}
	go m.storeEntities(email, e)
	raw := m.parseMessage(email, e, msg)
	m.bufferMessage(email, chatKey, raw)
	logger.Debugf("[TG-EVENT][%s] %s: %s", email, chatKey, raw.Text)
}

// parseMessage maps a *tg.Message into types.RawMessage. Sender display name
// resolves from the entities bundle first, then falls back to the persisted
// contact cache so the scanner never sees a bare numeric ID when we've seen
// this user before.
func (m *TelegramManager) parseMessage(email string, e tg.Entities, msg *tg.Message) types.RawMessage {
	senderID, senderName := resolveSender(e, msg)
	if senderName == "" && senderID != "" {
		senderName = store.GetNameByTelegramID(email, senderID)
	}
	var replyToID string
	if h, ok := msg.ReplyTo.(*tg.MessageReplyHeader); ok {
		if id, have := h.GetReplyToMsgID(); have {
			replyToID = strconv.Itoa(id)
		}
	}

	return types.RawMessage{
		ID:            strconv.Itoa(msg.ID),
		Sender:        senderID,
		SenderName:    senderName,
		Text:          msg.Message,
		Timestamp:     time.Unix(int64(msg.Date), 0),
		ReplyToID:     replyToID,
		IsFromMe:      msg.Out,
		HasAttachment: msg.Media != nil,
	}
}

// storeEntities walks the dispatched users/chats/channels and (a) persists user
// display names to `contacts` for auto-retroactive v_messages resolution,
// (b) caches chat/channel titles in groupCache for GetGroupName. Runs async.
func (m *TelegramManager) storeEntities(email string, e tg.Entities) {
	ctx := context.Background()
	for id, u := range e.Users {
		name := buildUserDisplayName(u)
		uid := strconv.FormatInt(id, 10)
		if name != "" {
			_ = store.SaveTelegramContact(ctx, email, uid, name)
			m.groupCache.Store(fmt.Sprintf("tg_user_%d", id), name)
		}
	}
	for id, c := range e.Chats {
		if c.Title != "" {
			m.groupCache.Store(fmt.Sprintf("tg_chat_%d", id), c.Title)
		}
	}
	for id, c := range e.Channels {
		if c.Title != "" {
			m.groupCache.Store(fmt.Sprintf("tg_channel_%d", id), c.Title)
		}
	}
}

func buildUserDisplayName(u *tg.User) string {
	name := u.FirstName
	if u.LastName != "" {
		if name != "" {
			name += " "
		}
		name += u.LastName
	}
	if name == "" {
		name = u.Username
	}
	return name
}

// peerKey converts the message's PeerID into a stable scanner-facing string key.
// Prefixes ("tg_user_" / "tg_chat_" / "tg_channel_") distinguish DM vs group later.
func peerKey(p tg.PeerClass) (string, bool) {
	switch v := p.(type) {
	case *tg.PeerUser:
		return fmt.Sprintf("tg_user_%d", v.UserID), true
	case *tg.PeerChat:
		return fmt.Sprintf("tg_chat_%d", v.ChatID), true
	case *tg.PeerChannel:
		return fmt.Sprintf("tg_channel_%d", v.ChannelID), true
	default:
		return "", false
	}
}

// resolveSender returns (senderID, senderName). Missing FromID falls back to PeerID
// (DM case where the whole chat is the sender).
func resolveSender(e tg.Entities, msg *tg.Message) (string, string) {
	if from, ok := msg.GetFromID(); ok {
		if pu, ok := from.(*tg.PeerUser); ok {
			return strconv.FormatInt(pu.UserID, 10), userName(e, pu.UserID)
		}
	}
	if pu, ok := msg.PeerID.(*tg.PeerUser); ok {
		return strconv.FormatInt(pu.UserID, 10), userName(e, pu.UserID)
	}
	return "", ""
}

func userName(e tg.Entities, id int64) string {
	u, ok := e.Users[id]
	if !ok {
		return ""
	}
	return buildUserDisplayName(u)
}

// bufferMessage appends raw into email→chatKey circular buffer (cap 200).
func (m *TelegramManager) bufferMessage(email, chatKey string, raw types.RawMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.messageBuffer[email]; !ok {
		m.messageBuffer[email] = make(map[string][]types.RawMessage)
	}
	buf := append(m.messageBuffer[email][chatKey], raw)
	if len(buf) > 200 {
		buf = buf[len(buf)-200:]
	}
	m.messageBuffer[email][chatKey] = buf
}

// PopMessages atomically drains every chat buffer for the given user.
func (m *TelegramManager) PopMessages(email string) map[string][]types.RawMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	userBuf, ok := m.messageBuffer[email]
	if !ok || len(userBuf) == 0 {
		return nil
	}

	out := make(map[string][]types.RawMessage, len(userBuf))
	for k, msgs := range userBuf {
		if len(msgs) > 0 {
			out[k] = msgs
		}
	}
	m.messageBuffer[email] = make(map[string][]types.RawMessage)
	return out
}

func (m *TelegramManager) dropBuffer(email string) {
	m.mu.Lock()
	delete(m.messageBuffer, email)
	m.mu.Unlock()
}

// GetGroupName returns a human-friendly label for a chatKey. Resolution order:
//   1. in-memory groupCache (populated by storeEntities + hydrateDialogs)
//   2. persisted contact cache (DMs only — mirrors WhatsApp's store lookup)
//   3. live MessagesGetChats RPC for basic chats (no access_hash required)
//   4. numeric tail fallback
func (m *TelegramManager) GetGroupName(email string, chatKey string) string {
	if v, ok := m.groupCache.Load(chatKey); ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	if strings.HasPrefix(chatKey, "tg_user_") {
		uid := strings.TrimPrefix(chatKey, "tg_user_")
		if name := store.GetNameByTelegramID(email, uid); name != "" {
			m.groupCache.Store(chatKey, name)
			return name
		}
	}
	if strings.HasPrefix(chatKey, "tg_chat_") {
		idStr := strings.TrimPrefix(chatKey, "tg_chat_")
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			if title := m.lookupBasicChatTitle(email, id); title != "" {
				m.groupCache.Store(chatKey, title)
				return title
			}
		}
	}
	for _, prefix := range []string{"tg_user_", "tg_chat_", "tg_channel_"} {
		if strings.HasPrefix(chatKey, prefix) {
			return strings.TrimPrefix(chatKey, prefix)
		}
	}
	return chatKey
}

// hydrateDialogs fetches the dialog list once on connect to seed groupCache and
// `contacts` with every peer we can reach, so GetGroupName resolves titles for
// dormant chats that won't push a new message through ingestMessage.
func (m *TelegramManager) hydrateDialogs(ctx context.Context, client *telegram.Client, email string) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := client.API().MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		OffsetPeer: &tg.InputPeerEmpty{},
		Limit:      200,
	})
	if err != nil {
		logger.Warnf("[TG-HYDRATE][%s] GetDialogs: %v", email, err)
		return
	}

	e := tg.Entities{
		Users:    make(map[int64]*tg.User),
		Chats:    make(map[int64]*tg.Chat),
		Channels: make(map[int64]*tg.Channel),
	}
	var users []tg.UserClass
	var chats []tg.ChatClass
	switch d := resp.(type) {
	case *tg.MessagesDialogs:
		users, chats = d.Users, d.Chats
	case *tg.MessagesDialogsSlice:
		users, chats = d.Users, d.Chats
	default:
		return
	}
	for _, u := range users {
		if user, ok := u.(*tg.User); ok {
			e.Users[user.ID] = user
		}
	}
	for _, c := range chats {
		switch v := c.(type) {
		case *tg.Chat:
			e.Chats[v.ID] = v
		case *tg.Channel:
			e.Channels[v.ID] = v
		}
	}
	m.storeEntities(email, e)
	logger.Infof("[TG-HYDRATE][%s] cached %d users, %d chats, %d channels", email, len(e.Users), len(e.Chats), len(e.Channels))
}

// lookupBasicChatTitle performs a live gotd RPC to resolve a legacy (non-channel)
// chat's title. Channels/supergroups require an access_hash we don't have on a
// cold miss, so this path only handles tg_chat_* keys.
func (m *TelegramManager) lookupBasicChatTitle(email string, chatID int64) string {
	m.mu.RLock()
	client, ok := m.clients[email]
	m.mu.RUnlock()
	if !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := client.API().MessagesGetChats(ctx, []int64{chatID})
	if err != nil {
		return ""
	}
	for _, c := range resp.GetChats() {
		if chat, ok := c.(*tg.Chat); ok && chat.ID == chatID && chat.Title != "" {
			return chat.Title
		}
	}
	return ""
}
