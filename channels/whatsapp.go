package channels

import (
	"context"
	"encoding/base64"
	"fmt"
	"message-consolidator/config"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waTypes "go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type WAManager struct {
	clients  map[string]*whatsmeow.Client
	chatBuf  *ChatBuffer
	latestQR map[string]string
	mu       sync.RWMutex
	container     *sqlstore.Container
	containerOnce sync.Once

	//Why: Uses callback functions to decouple the WhatsApp manager from specific store or notification logic, improving testability.
	FetchUserWAJID func(email string) (string, error)
	OnConnected    func(email, wajid string)
	OnLoggedOut    func(email string)
}

func NewWAManager() *WAManager {
	return &WAManager{
		clients:        make(map[string]*whatsmeow.Client),
		chatBuf:        newChatBuffer(),
		latestQR:       make(map[string]string),
		FetchUserWAJID: func(email string) (string, error) { return "", nil },
		OnConnected:    func(email, wajid string) {},
		OnLoggedOut:    func(email string) {},
	}
}

var DefaultWAManager = NewWAManager()

func (m *WAManager) getLogLevel(cfg *config.Config) string {
	logLevel := "INFO"
	if cfg != nil {
		if strings.ToUpper(cfg.LogLevel) == "DEBUG" {
			logLevel = "DEBUG"
		} else if strings.ToUpper(cfg.LogLevel) == "ERROR" {
			logLevel = "ERROR"
		}
	}
	return logLevel
}

// Why: Encapsulates the container initialization logic to keep the main setup flow clean and strictly separated.
func (m *WAManager) initContainer(cfg *config.Config) {
	m.containerOnce.Do(func() {
		//Why: Replicates a standard Chrome/macOS browsing session in the device properties to minimize the risk of being blocked by WhatsApp's anti-automated-linking checks.
		waStore.SetOSInfo("Mac OS", [3]uint32{10, 15, 7})
		pType := waCompanionReg.DeviceProps_CHROME
		waStore.DeviceProps.PlatformType = &pType

		dbLog := waLog.Stdout("Database", m.getLogLevel(cfg), true)
		m.container = sqlstore.NewWithDB(store.GetDB(), "sqlite3", dbLog)
		//Why: Forces a database schema upgrade to ensure the WhatsApp message store remains compatible with the current version of the library.
		if err := m.container.Upgrade(context.Background()); err != nil {
			logger.Errorf("[WA] store upgrade failed: %v", err)
		}
	})
}

func (m *WAManager) InitWhatsApp(email string, cfg *config.Config) {
	m.mu.Lock()
	if _, ok := m.clients[email]; ok {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	var err error
	m.initContainer(cfg)

	if m.container == nil {
		logger.Errorf("[WA] store permanently failed for %s", email)
		return
	}

	//Why: Retrieves the previously associated WhatsApp JID for the user to attempt a session restoration without requiring a new QR scan.
	wajid, err := m.FetchUserWAJID(email)
	if err != nil {
		logger.Warnf("[WA] failed to fetch WAJID for %s: %v", email, err)
		return
	}

	var device *waStore.Device
	if wajid != "" {
		jid, _ := waTypes.ParseJID(wajid)
		device, err = m.container.GetDevice(context.Background(), jid)
		if err != nil {
			logger.Errorf("[WA] device store failed for %s (JID: %s): %v", email, wajid, err)
		}
	}

	if device == nil {
		device = m.container.NewDevice()
	}

	clientLog := waLog.Stdout("Client", m.getLogLevel(cfg), true)
	client := whatsmeow.NewClient(device, clientLog)

	m.mu.Lock()
	m.clients[email] = client
	m.mu.Unlock()

	// any 사유: whatsmeow.Client.AddEventHandler 콜백 시그니처(`func(any)`) — 내부 type switch로 디스패치.
	client.AddEventHandler(func(evt any) {
		m.handleEvent(email, client, evt)
	})

	if client.Store.ID == nil {
		logger.Infof("[WA] no existing session for %s, please scan QR code", email)
		return
	}
	logger.Infof("[WA] found existing session for %s, connecting...", email)
	if err = client.Connect(); err != nil {
		logger.Warnf("[WA] connect failed for %s: %v", email, err)
		return
	}
	logger.Infof("[WA] connected successfully for %s", email)
	if err := client.SendPresence(context.Background(), waTypes.PresenceAvailable); err != nil {
		logger.Warnf("[WA] SendPresence failed for %s: %v", email, err)
	}
}

func (m *WAManager) GetQR(ctx context.Context, email string) (string, error) {
	m.mu.RLock()
	client, ok1 := m.clients[email]
	m.mu.RUnlock()

	if !ok1 {
		return "", fmt.Errorf("client not initialized for %s", email)
	}

	if client.IsConnected() && client.IsLoggedIn() {
		return "CONNECTED", nil
	}

	//Why: Ensures the QR code channel is initialized before establishing the connection, as required by the underlying WhatsApp library for proper pairing flow.
	if client.IsConnected() {
		logger.Infof("[WA] qr: client already connected for %s, disconnecting to get QR channel...", email)
		client.Disconnect()
	}

	qrChan, err := client.GetQRChannel(ctx)
	if err != nil {
		logger.Errorf("[WA] failed to get QR channel for %s: %v", email, err)
		return "", fmt.Errorf("failed to get QR channel for %s: %w", email, err)
	}

	if err := client.Connect(); err != nil {
		logger.Errorf("[WA] failed to connect client for %s: %v", email, err)
		return "", fmt.Errorf("failed to connect for %s: %w", email, err)
	}

	return m.consumeQRChannel(ctx, email, qrChan)
}

// Why: Splits the QR-event consumption loop out of GetQR so the parent function stays in cognitive budget; switch is intrinsic to the upstream event protocol.
func (m *WAManager) consumeQRChannel(ctx context.Context, email string, qrChan <-chan whatsmeow.QRChannelItem) (string, error) {
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case evt, ok := <-qrChan:
			if !ok {
				return "", fmt.Errorf("QR channel closed")
			}
			result, done, err := m.handleQREvent(email, evt)
			if done {
				return result, err
			}
		}
	}
}

func (m *WAManager) handleQREvent(email string, evt whatsmeow.QRChannelItem) (string, bool, error) {
	switch evt.Event {
	case "code":
		png, err := qrcode.Encode(evt.Code, qrcode.High, 300)
		if err != nil {
			logger.Errorf("[WA] failed to encode QR for %s: %v", email, err)
			return "", true, fmt.Errorf("failed to encode QR: %w", err)
		}
		encoded := base64.StdEncoding.EncodeToString(png)
		m.mu.Lock()
		m.latestQR[email] = encoded
		m.mu.Unlock()
		logger.Infof("[WA] qr generated for %s (len: %d)", email, len(encoded))
		return encoded, true, nil
	case "success":
		logger.Infof("[WA] qr scan success for %s", email)
		return "CONNECTED", true, nil
	default:
		logger.Debugf("[WA] unknown qr event for %s: %s", email, evt.Event)
		return "", false, nil
	}
}

func (m *WAManager) GetStatus(email string) string {
	m.mu.RLock()
	client, ok := m.clients[email]
	m.mu.RUnlock()

	if !ok {
		return "disconnected"
	}
	if client.IsConnected() && client.IsLoggedIn() {
		return "connected"
	}
	return "disconnected"
}

func (m *WAManager) LogoutWhatsApp(ctx context.Context, email string) error {
	m.mu.Lock()
	client, ok := m.clients[email]
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("client not initialized for %s", email)
	}

	if client.IsConnected() {
		err := client.Logout(ctx)
		if err != nil {
			logger.Errorf("[WA] logout failed for %s: %v", email, err)
			return err
		}
	}

	m.mu.Lock()
	delete(m.clients, email)
	delete(m.latestQR, email)
	m.mu.Unlock()
	m.chatBuf.drop(email)

	logger.Infof("[WA] logout cleanup for %s complete", email)
	return nil
}

func (m *WAManager) GetGroupName(email string, jidStr string) string {
	jid, _ := waTypes.ParseJID(jidStr)
	m.mu.RLock()
	client, ok := m.clients[email]
	m.mu.RUnlock()

	if !ok {
		return jid.User
	}

	if jid.Server == "g.us" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		info, err := client.GetGroupInfo(ctx, jid)
		if err == nil && info.Name != "" {
			return info.Name
		}
	}
	return jid.User
}

// Why: Provides a static way to resolve mentions in text if the explicit JID list is lost, though metadata-based resolution is preferred.
func ResolveWAMentions(email, text string, jids []string) string {
	if len(jids) == 0 {
		return text
	}
	result := text
	for _, jidStr := range jids {
		jid, _ := waTypes.ParseJID(jidStr)
		name := store.GetNameByWhatsAppNumber(email, jid.User)
		if name != "" {
			result = strings.ReplaceAll(result, "@"+jid.User, "@"+name)
		}
	}
	return result
}

func (m *WAManager) PopMessages(email string) map[string][]types.RawMessage {
	return m.chatBuf.pop(email)
}

// GetDeviceName returns the linked WhatsApp device's PushName (or Platform as fallback).
// Empty when no client exists or pairing has not completed — caller decides whether to surface that.
func (m *WAManager) GetDeviceName(email string) string {
	m.mu.RLock()
	client, ok := m.clients[email]
	m.mu.RUnlock()
	if !ok || client == nil || client.Store == nil || client.Store.ID == nil {
		return ""
	}
	if client.Store.PushName != "" {
		return client.Store.PushName
	}
	return client.Store.Platform
}

// Why: Provides simplified global access points to the WhatsApp manager instance for common operations like status checks and logging out.
func GetWhatsAppStatus(email string) string {
	return DefaultWAManager.GetStatus(email)
}

func GetWhatsAppDeviceName(email string) string {
	return DefaultWAManager.GetDeviceName(email)
}

func GetWhatsAppQR(ctx context.Context, email string) (string, error) {
	return DefaultWAManager.GetQR(ctx, email)
}

func LogoutWhatsApp(ctx context.Context, email string) error {
	return DefaultWAManager.LogoutWhatsApp(ctx, email)
}

func DisconnectAllWhatsApp() {
	DefaultWAManager.mu.Lock()

	type waClientInfo struct {
		email  string
		client *whatsmeow.Client
	}
	var clientsToDisconnect []waClientInfo

	for email, client := range DefaultWAManager.clients {
		if client.IsConnected() {
			clientsToDisconnect = append(clientsToDisconnect, waClientInfo{email: email, client: client})
		}
	}
	DefaultWAManager.mu.Unlock()

	if len(clientsToDisconnect) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, info := range clientsToDisconnect {
		wg.Add(1)
		go func(email string, c *whatsmeow.Client) {
			defer wg.Done()
			defer safego.Recover("wa-disconnect-client")
			logger.Infof("[WA] Disconnecting client for %s...", email)
			c.Disconnect()
		}(info.email, info.client)
	}

	//Why: Disconnect external clients concurrently with a timeout to prevent network issues from hanging the entire application shutdown.
	done := make(chan struct{})
	go func() {
		defer safego.Recover("wa-disconnect-wait")
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Infof("[WA] All WhatsApp clients disconnected successfully.")
	case <-time.After(2 * time.Second):
		logger.Warnf("[WA] Timeout reached while disconnecting WhatsApp clients.")
	}
}

func (m *WAManager) extractMediaInfo(msg *waProto.Message) (string, bool, []string) {
	if msg == nil {
		return "", false, nil
	}
	if msg.ImageMessage != nil {
		return "[Image]", true, []string{"image.jpg"}
	}
	if msg.DocumentMessage != nil {
		name := msg.DocumentMessage.GetFileName()
		if name == "" {
			name = "document"
		}
		return fmt.Sprintf("[Document: %s]", name), true, []string{name}
	}
	if msg.VideoMessage != nil {
		return "[Video]", true, []string{"video.mp4"}
	}
	if msg.AudioMessage != nil {
		return "[Audio]", true, []string{"audio.ogg"}
	}
	return "", false, nil
}
