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

func getWALogLevel() string {
	logLevel := waLogLevel
	if cfg != nil {
		if strings.ToUpper(cfg.LogLevel) == "DEBUG" {
			logLevel = "DEBUG"
		} else if strings.ToUpper(cfg.LogLevel) == "ERROR" {
			logLevel = "ERROR"
		}
	}
	return logLevel
}

func InitWhatsApp(email string) {
	waBufferMu.Lock()
	if _, ok := waClients[email]; ok {
		waBufferMu.Unlock()
		return
	}
	waBufferMu.Unlock()

	var err error
	waContainerOnce.Do(func() {
		dbLog := waLog.Stdout("Database", getWALogLevel(), true)
		dbURL := cfg.NeonDBURL
		if dbURL == "" {
			logger.Debugf("[WA-INIT] NeonDB URL is empty in config")
			return
		}

		// Retry logic for DB connection (helpful during cold starts or high-load migrations)
		maxRetries := 5
		for i := 1; i <= maxRetries; i++ {
			waContainer, err = sqlstore.New(context.Background(), "postgres", dbURL, dbLog)
			if err == nil {
				break
			}
			logger.Warnf("WA Store init attempt %d failed: %v. Retrying...", i, err)
			if i < maxRetries {
				time.Sleep(2 * time.Second)
			}
		}
	})

	if err != nil || waContainer == nil {
		logger.Errorf("WA Store permanently failed for %s: %v", email, err)
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

	clientLog := waLog.Stdout("Client", getWALogLevel(), true)
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
		waBufferMu.Lock()
		if _, ok := waMessageBuffer[email]; !ok {
			waMessageBuffer[email] = make(map[types.JID][]appStore.RawChatMessage)
		}

		// Map 조회를 최소화하기 위해 변수 사용
		chatBuffer := waMessageBuffer[email][v.Info.Chat]
		chatBuffer = append(chatBuffer, appStore.RawChatMessage{
			ID:        v.Info.ID,
			User:      sender,
			Sender:    sender,
			Text:      msgText,
			Timestamp: v.Info.Timestamp,
			Time:      v.Info.Timestamp,
			RawTS:     v.Info.ID,
		})

		// 버퍼 크기 제한 (최신 200개)
		if len(chatBuffer) > 200 {
			chatBuffer = chatBuffer[len(chatBuffer)-200:]
		}
		waMessageBuffer[email][v.Info.Chat] = chatBuffer
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
