package channels

import (
	"context"
	"sync"
	"time"

	"message-consolidator/internal/whataphttpx"
	"message-consolidator/logger"

	"github.com/line/line-bot-sdk-go/v8/linebot/messaging_api"
)

// profileCacheTTL controls how long a resolved display name is kept before re-fetching.
// Why: LINE GetProfile is a paid API call; prime TTL avoids hammering it within a single scan cycle.
const profileCacheTTL = 53 * time.Minute

// profileEntry holds a cached display name with an expiry timestamp.
type profileEntry struct {
	name    string
	expires time.Time
}

// LineManager manages a shared LINE Messaging API client and a sender-name cache.
// Unlike Telegram/WhatsApp, LINE is a single-bot (no per-user connection) so no
// per-email client map is needed.
// LineManager manages a shared LINE Messaging API client and a sender-name cache.
// Why: LINE webhook handler calls store.InsertLineInbox directly (push model),
// so no OnMessage callback chain is needed unlike WhatsApp/Telegram session models.
type LineManager struct {
	mu         sync.RWMutex
	bot        *messaging_api.MessagingApiAPI
	profileMu  sync.RWMutex
	profileMap map[string]profileEntry
}

// DefaultLineManager is the process-wide singleton used by the handler and scanner.
var DefaultLineManager = newLineManager()

func newLineManager() *LineManager {
	return &LineManager{
		profileMap: make(map[string]profileEntry),
	}
}

// Init (re-)initializes the LINE bot client with the provided credentials.
// Safe to call multiple times; replaces the existing client.
func (m *LineManager) Init(channelToken string) error {
	client, err := messaging_api.NewMessagingApiAPI(
		channelToken,
		messaging_api.WithHTTPClient(whataphttpx.Client()),
	)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.bot = client
	m.mu.Unlock()
	return nil
}

// IsReady reports whether the manager has been initialized with valid credentials.
func (m *LineManager) IsReady() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.bot != nil
}

// GetStatus returns "connected" when credentials are loaded, "disconnected" otherwise.
func (m *LineManager) GetStatus() string {
	if m.IsReady() {
		return "connected"
	}
	return "disconnected"
}

// ResolveSenderName returns the LINE display name for userID.
// Results are cached for profileCacheTTL to reduce API calls.
func (m *LineManager) ResolveSenderName(userID string) string {
	if userID == "" {
		return ""
	}
	m.profileMu.RLock()
	if entry, ok := m.profileMap[userID]; ok && time.Now().Before(entry.expires) {
		m.profileMu.RUnlock()
		return entry.name
	}
	m.profileMu.RUnlock()

	m.mu.RLock()
	bot := m.bot
	m.mu.RUnlock()
	if bot == nil {
		return userID
	}

	profile, err := bot.GetProfile(userID)
	if err != nil {
		logger.Debugf("[LINE] GetProfile(%s) failed: %v", userID, err)
		return userID
	}

	name := profile.DisplayName
	m.profileMu.Lock()
	m.profileMap[userID] = profileEntry{name: name, expires: time.Now().Add(profileCacheTTL)}
	m.profileMu.Unlock()
	return name
}

// Reset removes the current bot client (called on credential deletion).
func (m *LineManager) Reset() {
	m.mu.Lock()
	m.bot = nil
	m.mu.Unlock()
}

// GetLINEStatus returns the connection status for the singleton manager.
func GetLINEStatus() string {
	return DefaultLineManager.GetStatus()
}

// InitLINE initialises the singleton manager. Called from main.go after config load.
func InitLINE(ctx context.Context, channelToken string) {
	if channelToken == "" {
		return
	}
	if err := DefaultLineManager.Init(channelToken); err != nil { //nolint:contextcheck // whataphttpx.Client takes no ctx by design; trace rides on http.Request.Context (see package doc)
		logger.Errorf("[LINE] failed to init bot client: %v", err)
		return
	}
	logger.Infof("[LINE] bot client ready")
}
