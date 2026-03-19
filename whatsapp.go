package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"message-consolidator/logger"
	appStore "message-consolidator/store"
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

var (
	waClients       = make(map[string]*whatsmeow.Client)
	waMessageBuffer = make(map[string]map[types.JID][]appStore.RawChatMessage)
	waLatestQR      = make(map[string]string)
	waBufferMu      sync.RWMutex
	waLogLevel      = "INFO"
	waContainer     *sqlstore.Container
	waContainerOnce sync.Once
)

func InitWhatsApp(email string) {
	waBufferMu.Lock()
	if _, ok := waClients[email]; ok {
		waBufferMu.Unlock()
		return
	}
	waBufferMu.Unlock()

	var err error
	waContainerOnce.Do(func() {
		logLevel := waLogLevel
		if cfg != nil {
			if strings.ToUpper(cfg.LogLevel) == "DEBUG" {
				logLevel = "DEBUG"
			} else if strings.ToUpper(cfg.LogLevel) == "ERROR" {
				logLevel = "ERROR"
			}
		}
		dbLog := waLog.Stdout("Database", logLevel, true)
		dbURL := cfg.NeonDBURL
		if dbURL == "" {
			logger.Debugf("[WA-INIT] NeonDB URL is empty in config")
			return
		}
		waContainer, err = sqlstore.New(context.Background(), "postgres", dbURL, dbLog)
	})

	if err != nil || waContainer == nil {
		logger.Infof("WA Store failed for %s: %v", email, err)
		return
	}

	// Load user to get WAJID
	user, err := appStore.GetOrCreateUser(email, "", "")
	if err != nil {
		logger.Infof("InitWA: User %s not found in DB: %v", email, err)
		return
	}

	var device *store.Device
	if user.WAJID != "" {
		jid, _ := types.ParseJID(user.WAJID)
		device, err = waContainer.GetDevice(context.Background(), jid)
		if err != nil {
			logger.Debugf("WA Device Store failed for %s (JID: %s): %v", email, user.WAJID, err)
		}
	}

	if device == nil {
		device = waContainer.NewDevice()
	}

	logLevel := waLogLevel
	if cfg != nil {
		if strings.ToUpper(cfg.LogLevel) == "DEBUG" {
			logLevel = "DEBUG"
		} else if strings.ToUpper(cfg.LogLevel) == "ERROR" {
			logLevel = "ERROR"
		}
	}
	clientLog := waLog.Stdout("Client", logLevel, true)
	client := whatsmeow.NewClient(device, clientLog)

	waBufferMu.Lock()
	waClients[email] = client
	if _, ok := waMessageBuffer[email]; !ok {
		waMessageBuffer[email] = make(map[types.JID][]appStore.RawChatMessage)
	}
	waBufferMu.Unlock()

	client.AddEventHandler(func(evt interface{}) {
		handleWhatsAppEvent(email, client, evt)
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

func handleWhatsAppEvent(email string, client *whatsmeow.Client, evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		if v.Info.IsFromMe {
			return
		}

		waBufferMu.Lock()
		if _, ok := waMessageBuffer[email]; !ok {
			waMessageBuffer[email] = make(map[types.JID][]appStore.RawChatMessage)
		}

		sender := v.Info.Sender.String()
		if v.Info.PushName != "" {
			sender = v.Info.PushName
		}

		msgText := ""
		if v.Message.GetConversation() != "" {
			msgText = v.Message.GetConversation()
		} else if v.Message.GetExtendedTextMessage().GetText() != "" {
			msgText = v.Message.GetExtendedTextMessage().GetText()
		}

		if msgText != "" {
			waMessageBuffer[email][v.Info.Chat] = append(waMessageBuffer[email][v.Info.Chat], appStore.RawChatMessage{
				ID:        v.Info.ID,
				User:      sender,
				Sender:    sender,
				Text:      msgText,
				Timestamp: v.Info.Timestamp,
				Time:      v.Info.Timestamp,
				RawTS:     v.Info.ID,
			})
			if len(waMessageBuffer[email][v.Info.Chat]) > 200 {
				waMessageBuffer[email][v.Info.Chat] = waMessageBuffer[email][v.Info.Chat][len(waMessageBuffer[email][v.Info.Chat])-200:]
			}
		}
		waBufferMu.Unlock()
		logger.Debugf("[WA-EVENT][%s] Message from %s (Chat: %s): %s", email, sender, v.Info.Chat, msgText)

	case *events.Connected:
		logger.Debugf("[WA-EVENT][%s] Connected to WhatsApp", email)
		if client.Store.ID != nil {
			appStore.UpdateUserWAJID(email, client.Store.ID.String())
		}
	case *events.OfflineSyncCompleted:
		logger.Debugf("[WA-EVENT][%s] Offline sync completed", email)
	case *events.LoggedOut:
		logger.Debugf("[WA-EVENT][%s] Logged out from WhatsApp", email)
		appStore.UpdateUserWAJID(email, "")
		waBufferMu.Lock()
		delete(waClients, email)
		delete(waMessageBuffer, email)
		delete(waLatestQR, email)
		waBufferMu.Unlock()
	default:
	}
}

func GetWhatsAppQR(ctx context.Context, email string) (string, error) {
	waBufferMu.RLock()
	client, ok1 := waClients[email]
	waBufferMu.RUnlock()

	if !ok1 {
		return "", fmt.Errorf("client not initialized for %s", email)
	}

	if client.IsConnected() && client.IsLoggedIn() {
		return "CONNECTED", nil
	}

	qrChan, err := client.GetQRChannel(ctx)
	if err != nil {
		logger.Debugf("[WA-TRACE][%s] GetQRChannel initial error: %v", email, err)
	}

	if !client.IsConnected() {
		if err := client.Connect(); err != nil {
			return "", fmt.Errorf("failed to connect for %s: %v", email, err)
		}
		if qrChan == nil {
			qrChan, err = client.GetQRChannel(ctx)
			if err != nil {
				return "", fmt.Errorf("failed to get QR after connect for %s: %v", email, err)
			}
		}
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
				waBufferMu.Lock()
				waLatestQR[email] = encoded
				waBufferMu.Unlock()
				return encoded, nil
			} else if evt.Event == "success" {
				return "CONNECTED", nil
			}
		}
	}
}

func GetWhatsAppStatus(email string) string {
	waBufferMu.RLock()
	client, ok := waClients[email]
	waBufferMu.RUnlock()

	if !ok {
		return "DISCONNECTED"
	}
	if client.IsConnected() && client.IsLoggedIn() {
		return "CONNECTED"
	}
	return "DISCONNECTED"
}

func GetGroupName(email string, jid types.JID) string {
	waBufferMu.RLock()
	client, ok := waClients[email]
	waBufferMu.RUnlock()

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

func GetWhatsAppClient(email string) *whatsmeow.Client {
	waBufferMu.RLock()
	defer waBufferMu.RUnlock()
	return waClients[email]
}
