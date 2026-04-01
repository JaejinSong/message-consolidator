package channels

import (
	"context"
	"encoding/base64"
	"fmt"
	"message-consolidator/config"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

var waMentionRegex = regexp.MustCompile(`@([0-9]+)`)

type WAManager struct {
	clients       map[string]*whatsmeow.Client
	messageBuffer map[string]map[waTypes.JID][]types.RawMessage
	latestQR      map[string]string
	mu            sync.RWMutex
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
		messageBuffer:  make(map[string]map[waTypes.JID][]types.RawMessage),
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
			logger.Errorf("WA Store upgrade failed: %v", err)
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
		logger.Errorf("WA Store permanently failed for %s", email)
		return
	}

	//Why: Retrieves the previously associated WhatsApp JID for the user to attempt a session restoration without requiring a new QR scan.
	wajid, err := m.FetchUserWAJID(email)
	if err != nil {
		logger.Infof("InitWA: Failed to fetch WAJID for %s: %v", email, err)
		return
	}

	var device *waStore.Device
	if wajid != "" {
		jid, _ := waTypes.ParseJID(wajid)
		device, err = m.container.GetDevice(context.Background(), jid)
		if err != nil {
			logger.Errorf("WA Device Store failed for %s (JID: %s): %v", email, wajid, err)
		}
	}

	if device == nil {
		device = m.container.NewDevice()
	}

	clientLog := waLog.Stdout("Client", m.getLogLevel(cfg), true)
	client := whatsmeow.NewClient(device, clientLog)

	m.mu.Lock()
	m.clients[email] = client
	if _, ok := m.messageBuffer[email]; !ok {
		m.messageBuffer[email] = make(map[waTypes.JID][]types.RawMessage)
	}
	m.mu.Unlock()

	client.AddEventHandler(func(evt interface{}) {
		m.handleEvent(email, client, evt)
	})

	if client.Store.ID == nil {
		logger.Infof("WA: No existing session found for %s, please scan QR code.", email)
	} else {
		logger.Infof("WA: Found existing session ID for %s, connecting...", email)
		err = client.Connect()
		if err != nil {
			logger.Infof("WA Connect failed for %s: %v", email, err)
		} else {
			logger.Infof("WA: Connected successfully for %s", email)
			client.SendPresence(context.Background(), waTypes.PresenceAvailable)
		}
	}
}

func (m *WAManager) handleEvent(email string, client *whatsmeow.Client, evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		m.handleMessageEvent(email, client, v)
	case *events.Connected:
		logger.Debugf("[WA-EVENT][%s] Connected to WhatsApp", email)
		if client.Store.ID != nil {
			m.OnConnected(email, client.Store.ID.String())
		}
	case *events.OfflineSyncCompleted:
		logger.Debugf("[WA-EVENT][%s] Offline sync completed", email)
	case *events.LoggedOut:
		logger.Debugf("[WA-EVENT][%s] Logged out from WhatsApp", email)
		m.OnLoggedOut(email)
		m.mu.Lock()
		delete(m.clients, email)
		delete(m.messageBuffer, email)
		delete(m.latestQR, email)
		m.mu.Unlock()
	default:
	}
}

// Why: Separates complex message handling logic from the main event router to improve readability and maintainability.
func (m *WAManager) handleMessageEvent(email string, client *whatsmeow.Client, msg *events.Message) {
	var msgText string
	var replyToID string
	var repliedToUser string

	if msg.Message.GetConversation() != "" {
		msgText = msg.Message.GetConversation()
	} else if extMsg := msg.Message.GetExtendedTextMessage(); extMsg != nil {
		msgText = extMsg.GetText()
		if extMsg.ContextInfo != nil {
			if extMsg.ContextInfo.StanzaID != nil {
				replyToID = *extMsg.ContextInfo.StanzaID
			}
			//Why: Extracts the original sender (Participant) of the quoted message to enable accurate 'Reply-to' context in AI task assignment.
			if extMsg.ContextInfo.Participant != nil {
				repliedJID, _ := waTypes.ParseJID(*extMsg.ContextInfo.Participant)
				repliedToUser = repliedJID.User // Fallback to number

				// Try to resolve name
				if name := store.GetNameByWhatsAppNumber(email, repliedJID.User); name != "" {
					repliedToUser = name
				} else if contact, err := client.Store.Contacts.GetContact(context.Background(), repliedJID); err == nil && (contact.PushName != "" || contact.FullName != "") {
					if contact.FullName != "" {
						repliedToUser = contact.FullName
					} else {
						repliedToUser = contact.PushName
					}
				}
			}
		}
	}

	if msgText == "" {
		return
	}

	sender := msg.Info.Sender.String()
	if msg.Info.IsFromMe {
		sender = "나" //Why: Explicitly marks self-sent messages as "나" (Me) so the AI analyst can correctly identify task status and ownership.
	} else if msg.Info.PushName != "" {
		sender = msg.Info.PushName
		go store.SaveWhatsAppContact(email, msg.Info.Sender.User, msg.Info.PushName)
	}

	msgText = m.resolveIncomingMentions(email, client, msgText)

	m.mu.Lock()
	if _, ok := m.messageBuffer[email]; !ok {
		m.messageBuffer[email] = make(map[waTypes.JID][]types.RawMessage)
	}

	//Why: Buffers incoming messages in memory to allow for batch processing and context-aware task extraction during the next scan cycle.
	chatBuffer := m.messageBuffer[email][msg.Info.Chat]
	chatBuffer = append(chatBuffer, types.RawMessage{
		ID:            msg.Info.ID,
		Sender:        sender,
		Text:          msgText,
		Timestamp:     msg.Info.Timestamp,
		ReplyToID:     replyToID,
		RepliedToUser: repliedToUser,
	})

	//Why: Caps the per-chat message buffer at 200 entries to prevent memory exhaustion in highly active group chats.
	if len(chatBuffer) > 200 {
		chatBuffer = chatBuffer[len(chatBuffer)-200:]
	}
	m.messageBuffer[email][msg.Info.Chat] = chatBuffer
	m.mu.Unlock()

	logger.Debugf("[WA-EVENT][%s] Message from %s (Chat: %s): %s", email, sender, msg.Info.Chat, msgText)
}

// Why: Resolves numeric WhatsApp mentions into human-readable contact names using local store data and WhatsApp's contact metadata for better AI analysis.
func (m *WAManager) resolveIncomingMentions(email string, client *whatsmeow.Client, text string) string {
	return waMentionRegex.ReplaceAllStringFunc(text, func(match string) string {
		number := match[1:]
		name := store.GetNameByWhatsAppNumber(email, number)
		if name != "" {
			return "@" + name
		}

		jid := waTypes.NewJID(number, waTypes.DefaultUserServer)
		if contact, err := client.Store.Contacts.GetContact(context.Background(), jid); err == nil {
			resolvedName := contact.PushName
			if contact.FullName != "" {
				resolvedName = contact.FullName
			} else if contact.BusinessName != "" {
				resolvedName = contact.BusinessName
			}

			if resolvedName != "" {
				go store.SaveWhatsAppContact(email, number, resolvedName)
				return "@" + resolvedName
			}
		}
		return match
	})
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
		logger.Infof("[WA-QR] Client already connected for %s, disconnecting to get QR channel...", email)
		client.Disconnect()
	}

	qrChan, err := client.GetQRChannel(ctx)
	if err != nil {
		logger.Errorf("[WA-QR] Failed to get QR channel for %s: %v", email, err)
		return "", fmt.Errorf("failed to get QR channel for %s: %v", email, err)
	}

	if err := client.Connect(); err != nil {
		logger.Errorf("[WA-QR] Failed to connect client for %s: %v", email, err)
		return "", fmt.Errorf("failed to connect for %s: %v", email, err)
	}

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case evt, ok := <-qrChan:
			if !ok {
				return "", fmt.Errorf("QR channel closed")
			}
			switch evt.Event {
			case "code":
				png, err := qrcode.Encode(evt.Code, qrcode.High, 300)
				if err != nil {
					logger.Errorf("[WA-QR] Failed to encode QR for %s: %v", email, err)
					return "", fmt.Errorf("failed to encode QR: %v", err)
				}
				encoded := base64.StdEncoding.EncodeToString(png)
				m.mu.Lock()
				m.latestQR[email] = encoded
				m.mu.Unlock()
				logger.Infof("[WA-QR] Generated new QR code for %s (len: %d)", email, len(encoded))
				return encoded, nil
			case "success":
				logger.Infof("[WA-QR] QR Scan success for %s", email)
				return "CONNECTED", nil
			default:
				logger.Debugf("[WA-QR] Received unknown QR event for %s: %s", email, evt.Event)
			}
		}
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

func (m *WAManager) LogoutWhatsApp(email string) error {
	m.mu.Lock()
	client, ok := m.clients[email]
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("client not initialized for %s", email)
	}

	if client.IsConnected() {
		err := client.Logout(context.Background())
		if err != nil {
			logger.Errorf("[WA-LOGOUT] Failed to logout for %s: %v", email, err)
			return err
		}
	}

	m.mu.Lock()
	delete(m.clients, email)
	delete(m.messageBuffer, email)
	delete(m.latestQR, email)
	m.mu.Unlock()

	logger.Infof("[WA-LOGOUT] Successfully logged out and cleaned up for %s", email)
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

func ResolveWAMentions(email, text string) string {
	//Why: Recognizes standard WhatsApp numeric mentions ("@12345678") for resolution into contact names.
	return waMentionRegex.ReplaceAllStringFunc(text, func(match string) string {
		number := match[1:]
		name := store.GetNameByWhatsAppNumber(email, number)
		if name != "" {
			return "@" + name
		}
		return match
	})
}

func (m *WAManager) GetClient(email string) *whatsmeow.Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.clients[email]
}

func (m *WAManager) PopMessages(email string) map[string][]types.RawMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	userBuffer, ok := m.messageBuffer[email]
	if !ok || len(userBuffer) == 0 {
		return nil
	}

	bufferCopy := make(map[string][]types.RawMessage)
	for jid, msgs := range userBuffer {
		if len(msgs) > 0 {
			bufferCopy[jid.String()] = msgs
		}
	}
	m.messageBuffer[email] = make(map[waTypes.JID][]types.RawMessage)
	return bufferCopy
}

// Why: Provides simplified global access points to the WhatsApp manager instance for common operations like status checks and logging out.
func GetWhatsAppStatus(email string) string {
	return DefaultWAManager.GetStatus(email)
}

func GetWhatsAppQR(ctx context.Context, email string) (string, error) {
	return DefaultWAManager.GetQR(ctx, email)
}

func LogoutWhatsApp(email string) error {
	return DefaultWAManager.LogoutWhatsApp(email)
}

func DisconnectAllWhatsApp() {
	DefaultWAManager.mu.Lock()
	
	type waClientInfo struct {
		email   string
		client  *whatsmeow.Client
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
			logger.Infof("[WA] Disconnecting client for %s...", email)
			c.Disconnect()
		}(info.email, info.client)
	}

	//Why: Disconnect external clients concurrently with a timeout to prevent network issues from hanging the entire application shutdown.
	done := make(chan struct{})
	go func() {
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
