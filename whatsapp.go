package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"message-consolidator/logger"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type WAManager struct {
	clients       map[string]*whatsmeow.Client
	messageBuffer map[string]map[types.JID][]RawMessage
	latestQR      map[string]string
	mu            sync.RWMutex
	container     *sqlstore.Container
	containerOnce sync.Once

	// Callbacks for Decoupling
	FetchUserWAJID func(email string) (string, error)
	OnConnected    func(email, wajid string)
	OnLoggedOut    func(email string)
}

func NewWAManager() *WAManager {
	return &WAManager{
		clients:        make(map[string]*whatsmeow.Client),
		messageBuffer:  make(map[string]map[types.JID][]RawMessage),
		latestQR:       make(map[string]string),
		FetchUserWAJID: func(email string) (string, error) { return "", nil },
		OnConnected:    func(email, wajid string) {},
		OnLoggedOut:    func(email string) {},
	}
}

var DefaultWAManager = NewWAManager()

func (m *WAManager) getLogLevel() string {
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

func (m *WAManager) InitClient(email string, dbURL string) {
	m.mu.Lock()
	if _, ok := m.clients[email]; ok {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	var err error
	m.containerOnce.Do(func() {
		dbLog := waLog.Stdout("Database", m.getLogLevel(), true)
		if dbURL == "" {
			logger.Debugf("[WA-INIT] NeonDB URL is empty in config")
			return
		}

		// Retry logic for DB connection (helpful during cold starts or high-load migrations)
		maxRetries := 5
		for i := 1; i <= maxRetries; i++ {
			m.container, err = sqlstore.New(context.Background(), "postgres", dbURL, dbLog)
			if err == nil {
				break
			}
			logger.Warnf("WA Store init attempt %d failed: %v. Retrying...", i, err)
			if i < maxRetries {
				time.Sleep(2 * time.Second)
			}
		}
	})

	if err != nil || m.container == nil {
		logger.Errorf("WA Store permanently failed for %s: %v", email, err)
		return
	}

	// Load user to get WAJID
	wajid, err := m.FetchUserWAJID(email)
	if err != nil {
		logger.Infof("InitWA: Failed to fetch WAJID for %s: %v", email, err)
		return
	}

	var device *store.Device
	if wajid != "" {
		jid, _ := types.ParseJID(wajid)
		device, err = m.container.GetDevice(context.Background(), jid)
		if err != nil {
			logger.Debugf("WA Device Store failed for %s (JID: %s): %v", email, wajid, err)
		}
	}

	if device == nil {
		device = m.container.NewDevice()
	}

	clientLog := waLog.Stdout("Client", m.getLogLevel(), true)
	client := whatsmeow.NewClient(device, clientLog)

	m.mu.Lock()
	m.clients[email] = client
	if _, ok := m.messageBuffer[email]; !ok {
		m.messageBuffer[email] = make(map[types.JID][]RawMessage)
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
			client.SendPresence(context.Background(), types.PresenceAvailable)
		}
	}
}

func (m *WAManager) handleEvent(email string, client *whatsmeow.Client, evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		if v.Info.IsFromMe {
			return
		}

		// 1. 메시지 텍스트 추출 (Lock 없이 수행 가능)
		msgText := v.Message.GetConversation()
		if msgText == "" {
			msgText = v.Message.GetExtendedTextMessage().GetText()
		}

		// 메시지가 비어있다면 처리 중단 (Lock 경합 방지)
		if msgText == "" {
			return
		}

		sender := v.Info.Sender.String()
		if v.Info.PushName != "" {
			sender = v.Info.PushName
		}

		// 2. 버퍼 업데이트를 위해 Write Lock 획득
		m.mu.Lock()
		if _, ok := m.messageBuffer[email]; !ok {
			m.messageBuffer[email] = make(map[types.JID][]RawMessage)
		}

		// Map 조회를 최소화하기 위해 변수 사용
		chatBuffer := m.messageBuffer[email][v.Info.Chat]
		chatBuffer = append(chatBuffer, RawMessage{
			ID:        v.Info.ID,
			Sender:    sender,
			Text:      msgText,
			Timestamp: v.Info.Timestamp,
		})

		// 버퍼 크기 제한 (최신 200개)
		if len(chatBuffer) > 200 {
			chatBuffer = chatBuffer[len(chatBuffer)-200:]
		}
		m.messageBuffer[email][v.Info.Chat] = chatBuffer
		m.mu.Unlock()

		logger.Debugf("[WA-EVENT][%s] Message from %s (Chat: %s): %s", email, sender, v.Info.Chat, msgText)

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

	// 먼저 연결 상태를 확인하고 필요하면 연결합니다.
	if !client.IsConnected() {
		if err := client.Connect(); err != nil {
			return "", fmt.Errorf("failed to connect for %s: %v", email, err)
		}
	}

	// 확실히 연결이 보장된 후 QR 채널을 단 1번만 요청합니다.
	qrChan, err := client.GetQRChannel(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get QR channel for %s: %v", email, err)
	}

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case evt, ok := <-qrChan:
			if !ok {
				return "", fmt.Errorf("QR channel closed")
			}
			if evt.Event == "code" {
				png, err := qrcode.Encode(evt.Code, qrcode.Medium, 256)
				if err != nil {
					return "", fmt.Errorf("failed to encode QR: %v", err)
				}
				encoded := "base64:" + base64.StdEncoding.EncodeToString(png)
				m.mu.Lock()
				m.latestQR[email] = encoded
				m.mu.Unlock()
				return encoded, nil
			} else if evt.Event == "success" {
				return "CONNECTED", nil
			}
		}
	}
}

func (m *WAManager) GetStatus(email string) string {
	m.mu.RLock()
	client, ok := m.clients[email]
	m.mu.RUnlock()

	if !ok {
		return "DISCONNECTED"
	}
	if client.IsConnected() && client.IsLoggedIn() {
		return "CONNECTED"
	}
	return "DISCONNECTED"
}

func (m *WAManager) GetGroupName(email string, jid types.JID) string {
	m.mu.RLock()
	client, ok := m.clients[email]
	m.mu.RUnlock()

	if !ok {
		return jid.String()
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

func (m *WAManager) GetClient(email string) *whatsmeow.Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.clients[email]
}

// PopMessages returns the accumulated messages for a user and clears the buffer.
func (m *WAManager) PopMessages(email string) map[string][]RawMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	userBuffer, ok := m.messageBuffer[email]
	if !ok || len(userBuffer) == 0 {
		return nil
	}

	bufferCopy := make(map[string][]RawMessage)
	for jid, msgs := range userBuffer {
		if len(msgs) > 0 {
			bufferCopy[jid.String()] = msgs
		}
	}
	m.messageBuffer[email] = make(map[types.JID][]RawMessage)
	return bufferCopy
}

// ---------------------------------------------------------
// 아래는 기존 다른 핸들러 파일들과의 호환성을 유지하기 위한 래퍼(Wrapper) 함수들입니다.
// 향후 다른 파일들도 `DefaultWAManager` 메서드를 직접 호출하도록 점진적으로 리팩토링할 수 있습니다.
// ---------------------------------------------------------

func InitWhatsApp(email string) {
	dbURL := ""
	if cfg != nil {
		dbURL = cfg.NeonDBURL
	}
	DefaultWAManager.InitClient(email, dbURL)
}

func GetWhatsAppQR(ctx context.Context, email string) (string, error) {
	return DefaultWAManager.GetQR(ctx, email)
}

func GetWhatsAppStatus(email string) string {
	return DefaultWAManager.GetStatus(email)
}

func GetGroupName(email string, jid types.JID) string {
	return DefaultWAManager.GetGroupName(email, jid)
}

func GetWhatsAppClient(email string) *whatsmeow.Client {
	return DefaultWAManager.GetClient(email)
}
