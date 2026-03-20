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

	// Callbacks for Decoupling
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

func (m *WAManager) InitWhatsApp(email string, dbURL string, cfg *config.Config) {
	m.mu.Lock()
	if _, ok := m.clients[email]; ok {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	var err error
	m.containerOnce.Do(func() {
		dbLog := waLog.Stdout("Database", m.getLogLevel(cfg), true)
		m.container = sqlstore.NewWithDB(store.GetDB(), "postgres", dbLog)
	})

	if err != nil || m.container == nil {
		logger.Errorf("WA Store permanently failed for %s: %v", email, err)
		return
	}

	// Load user to get WAJID via callback
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
			logger.Debugf("WA Device Store failed for %s (JID: %s): %v", email, wajid, err)
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
		if v.Info.IsFromMe {
			return
		}

		msgText := v.Message.GetConversation()
		if msgText == "" {
			msgText = v.Message.GetExtendedTextMessage().GetText()
		}

		if msgText == "" {
			return
		}

		sender := v.Info.Sender.String()
		if v.Info.PushName != "" {
			sender = v.Info.PushName
			go store.SaveWhatsAppContact(email, v.Info.Sender.User, v.Info.PushName)
		}

		msgText = waMentionRegex.ReplaceAllStringFunc(msgText, func(match string) string {
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

		m.mu.Lock()
		if _, ok := m.messageBuffer[email]; !ok {
			m.messageBuffer[email] = make(map[waTypes.JID][]types.RawMessage)
		}

		chatBuffer := m.messageBuffer[email][v.Info.Chat]
		chatBuffer = append(chatBuffer, types.RawMessage{
			ID:        v.Info.ID,
			Sender:    sender,
			Text:      msgText,
			Timestamp: v.Info.Timestamp,
		})

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

	if !client.IsConnected() {
		if err := client.Connect(); err != nil {
			logger.Errorf("[WA-QR] Failed to connect client for %s: %v", email, err)
			return "", fmt.Errorf("failed to connect for %s: %v", email, err)
		}
	}

	qrChan, err := client.GetQRChannel(ctx)
	if err != nil {
		logger.Errorf("[WA-QR] Failed to get QR channel for %s: %v", email, err)
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
			switch evt.Event {
			case "code":
				png, err := qrcode.Encode(evt.Code, qrcode.Medium, 256)
				if err != nil {
					return "", fmt.Errorf("failed to encode QR: %v", err)
				}
				encoded := "base64:" + base64.StdEncoding.EncodeToString(png)
				m.mu.Lock()
				m.latestQR[email] = encoded
				m.mu.Unlock()
				return encoded, nil
			case "success":
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
	// WhatsApp mentions are "@12345678"
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

// Top-level wrapper functions for easier access
func GetWhatsAppStatus(email string) string {
	return DefaultWAManager.GetStatus(email)
}

func GetWhatsAppQR(ctx context.Context, email string) (string, error) {
	return DefaultWAManager.GetQR(ctx, email)
}
