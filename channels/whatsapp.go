package channels

import (
	"context"
	"encoding/base64"
	"fmt"
	"message-consolidator/config"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
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
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
)



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
		return
	}
	logger.Infof("WA: Found existing session ID for %s, connecting...", email)
	if err = client.Connect(); err != nil {
		logger.Infof("WA Connect failed for %s: %v", email, err)
		return
	}
	logger.Infof("WA: Connected successfully for %s", email)
	if err := client.SendPresence(context.Background(), waTypes.PresenceAvailable); err != nil {
		logger.Warnf("[WA] SendPresence failed for %s: %v", email, err)
	}
}

func (m *WAManager) handleEvent(email string, client *whatsmeow.Client, evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		m.handleMessageEvent(email, client, v)
	case *events.Picture:
		// [Optimization] 프로필 사진 업데이트 이벤트 무시
		return
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

// isSystemMessage는 해당 메시지가 사용자 발화가 아닌 프로토콜/시스템 알림인지 판별합니다.
func isSystemMessage(msg *events.Message) bool {
	if msg == nil || msg.Message == nil {
		return true
	}
	// Why: [Protocol Filter] ProtocolMessage는 본문 없는 제어용 메시지(삭제, 동기화 등)이므로 제외합니다.
	if msg.Message.ProtocolMessage != nil || msg.Message.SenderKeyDistributionMessage != nil {
		return true
	}
	// Why: [Status/System Filter] 'status@broadcast' 발신자 또는 PushName이 없는 시스템 알림을 차단합니다.
	if msg.Info.Sender.User == "status" || (msg.Info.PushName == "" && !msg.Info.IsFromMe && !msg.Info.IsGroup) {
		return true
	}
	// Why: [Category Filter] Category가 'peer'인 메시지는 디바이스 간 통신용이므로 제외합니다.
	if msg.Info.Category == "peer" {
		return true
	}
	return false
}

// Why: Separates complex message handling logic from the main event router to improve readability and maintainability.
func (m *WAManager) handleMessageEvent(email string, client *whatsmeow.Client, msg *events.Message) {
	if isSystemMessage(msg) {
		return
	}

	msgText, meta, ok := m.parseMessageContent(email, client, msg)
	if !ok || msgText == "" {
		return
	}

	sender := m.resolveSenderName(email, client, msg.Info)
	msgText = m.resolveIncomingMentions(email, client, msgText, meta.MentionedIDs)

	m.bufferMessage(email, msg.Info.Chat, types.RawMessage{
		ID: msg.Info.ID, Sender: sender, Text: msgText,
		Timestamp: msg.Info.Timestamp, ReplyToID: meta.ReplyToID,
		RepliedToUser: meta.RepliedToUser, IsForwarded: meta.IsForwarded,
		IsFromMe: msg.Info.IsFromMe,
		MentionedIDs: meta.MentionedIDs, HasAttachment: meta.HasAttachment,
		AttachmentNames: meta.AttachmentNames,
	})

	logger.Debugf("[WA-EVENT][%s] Message from %s (Chat: %s): %s", email, sender, msg.Info.Chat, msgText)
}

type messageMetadata struct {
	ReplyToID       string
	RepliedToUser   string
	IsForwarded     bool
	MentionedIDs    []string
	HasAttachment   bool
	AttachmentNames []string
}

func (m *WAManager) parseMessageContent(email string, client *whatsmeow.Client, msg *events.Message) (string, messageMetadata, bool) {
	var meta messageMetadata
	var text string

	if conv := msg.Message.GetConversation(); conv != "" {
		return conv, meta, true
	}

	if ext := msg.Message.GetExtendedTextMessage(); ext != nil {
		text = ext.GetText()
		meta.IsForwarded = ext.ContextInfo.GetIsForwarded()
		meta.MentionedIDs = ext.ContextInfo.MentionedJID
		if ext.ContextInfo != nil {
			meta.ReplyToID = ext.ContextInfo.GetStanzaID()
			meta.RepliedToUser = m.resolveRepliedUser(email, client, ext.ContextInfo)
		}
		return text, meta, true
	}

	text, meta.HasAttachment, meta.AttachmentNames = m.extractMediaInfo(msg.Message)
	return text, meta, text != ""
}

func (m *WAManager) resolveSenderName(email string, client *whatsmeow.Client, info waTypes.MessageInfo) string {
	if info.IsFromMe {
		return email
	}
	if info.PushName != "" {
		go func(em, num, name string) {
			if err := store.SaveWhatsAppContact(context.Background(), em, num, name); err != nil {
				logger.Warnf("[WA] SaveWhatsAppContact failed for %s/%s: %v", em, num, err)
			}
		}(email, info.Sender.User, info.PushName)
		return info.PushName
	}
	return info.Sender.String()
}

func (m *WAManager) resolveRepliedUser(email string, client *whatsmeow.Client, ctx *waProto.ContextInfo) string {
	if ctx == nil || ctx.Participant == nil {
		return ""
	}
	repliedJID, _ := waTypes.ParseJID(*ctx.Participant)
	if name := store.GetNameByWhatsAppNumber(email, repliedJID.User); name != "" {
		return name
	}
	if contact, err := client.Store.Contacts.GetContact(context.Background(), repliedJID); err == nil {
		if contact.FullName != "" {
			return contact.FullName
		}
		return contact.PushName
	}
	return repliedJID.User
}

func (m *WAManager) bufferMessage(email string, chat waTypes.JID, raw types.RawMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.messageBuffer[email]; !ok {
		m.messageBuffer[email] = make(map[waTypes.JID][]types.RawMessage)
	}

	buffer := m.messageBuffer[email][chat]
	buffer = append(buffer, raw)
	if len(buffer) > 200 {
		buffer = buffer[len(buffer)-200:]
	}
	m.messageBuffer[email][chat] = buffer
}

// Why: Resolves numeric WhatsApp mentions into human-readable contact names using explicit MentionedJID metadata instead of fragile regex parsing.
func (m *WAManager) resolveIncomingMentions(email string, client *whatsmeow.Client, text string, jids []string) string {
	if len(jids) == 0 {
		return text
	}

	result := text
	for _, jidStr := range jids {
		jid, _ := waTypes.ParseJID(jidStr)
		number := jid.User
		placeholder := "@" + number

		resolvedName := m.resolveMentionName(email, client, jid, number)
		if resolvedName == "" {
			continue
		}
		//Why: Only replaces the specific numeric occurrence if we have high-confidence metadata from the API.
		result = strings.ReplaceAll(result, placeholder, "@"+resolvedName)
	}
	return result
}

//Why: Falls back to whatsmeow contact metadata in priority order (full → push → business) and persists asynchronously so the next mention skips the API hop.
func (m *WAManager) resolveMentionName(email string, client *whatsmeow.Client, jid waTypes.JID, number string) string {
	if name := store.GetNameByWhatsAppNumber(email, number); name != "" {
		return name
	}
	contact, err := client.Store.Contacts.GetContact(context.Background(), jid)
	if err != nil {
		return ""
	}
	resolved := pickContactName(contact)
	if resolved == "" {
		return ""
	}
	go func(em, num, name string) {
		if err := store.SaveWhatsAppContact(context.Background(), em, num, name); err != nil {
			logger.Warnf("[WA] SaveWhatsAppContact failed for %s/%s: %v", em, num, err)
		}
	}(email, number, resolved)
	return resolved
}

func pickContactName(c waTypes.ContactInfo) string {
	if c.FullName != "" {
		return c.FullName
	}
	if c.PushName != "" {
		return c.PushName
	}
	return c.BusinessName
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
		return "", fmt.Errorf("failed to get QR channel for %s: %w", email, err)
	}

	if err := client.Connect(); err != nil {
		logger.Errorf("[WA-QR] Failed to connect client for %s: %v", email, err)
		return "", fmt.Errorf("failed to connect for %s: %w", email, err)
	}

	return m.consumeQRChannel(ctx, email, qrChan)
}

//Why: Splits the QR-event consumption loop out of GetQR so the parent function stays in cognitive budget; switch is intrinsic to the upstream event protocol.
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
			logger.Errorf("[WA-QR] Failed to encode QR for %s: %v", email, err)
			return "", true, fmt.Errorf("failed to encode QR: %w", err)
		}
		encoded := base64.StdEncoding.EncodeToString(png)
		m.mu.Lock()
		m.latestQR[email] = encoded
		m.mu.Unlock()
		logger.Infof("[WA-QR] Generated new QR code for %s (len: %d)", email, len(encoded))
		return encoded, true, nil
	case "success":
		logger.Infof("[WA-QR] QR Scan success for %s", email)
		return "CONNECTED", true, nil
	default:
		logger.Debugf("[WA-QR] Received unknown QR event for %s: %s", email, evt.Event)
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

func LogoutWhatsApp(ctx context.Context, email string) error {
	return DefaultWAManager.LogoutWhatsApp(ctx, email)
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

func (m *WAManager) extractMediaInfo(msg *waProto.Message) (string, bool, []string) {
	if msg == nil { return "", false, nil }
	if msg.ImageMessage != nil {
		return "[Image]", true, []string{"image.jpg"}
	}
	if msg.DocumentMessage != nil {
		name := msg.DocumentMessage.GetFileName()
		if name == "" { name = "document" }
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
