package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"go.mau.fi/whatsmeow/proto/waE2E"
)

var (
	waClient        *whatsmeow.Client
	waLoginMu       sync.Mutex
	waMessageBuffer = make(map[types.JID][]RawChatMessage)
	waBufferMu      sync.RWMutex
	waLatestQR      string
	waQRTimer       *time.Timer
)

type CustomLogger struct {
	module string
}

func (c *CustomLogger) Debugf(format string, args ...interface{}) {
	log.Printf("[WA-DEBUG][%s] "+format, append([]interface{}{c.module}, args...)...)
}
func (c *CustomLogger) Infof(format string, args ...interface{}) {
	log.Printf("[WA-INFO][%s] "+format, append([]interface{}{c.module}, args...)...)
}
func (c *CustomLogger) Warnf(format string, args ...interface{}) {
	log.Printf("[WA-WARN][%s] "+format, append([]interface{}{c.module}, args...)...)
}
func (c *CustomLogger) Errorf(format string, args ...interface{}) {
	log.Printf("[WA-ERROR][%s] "+format, append([]interface{}{c.module}, args...)...)
}
func (c *CustomLogger) Sub(module string) waLog.Logger {
	return &CustomLogger{module: c.module + "/" + module}
}

func InitWhatsApp(ctx context.Context) {
	dbLog := &CustomLogger{module: "Database"}
	clientLog := &CustomLogger{module: "Client"}

	container, err := sqlstore.New(ctx, "postgres", cfg.NeonDBURL, dbLog)
	if err != nil {
		log.Printf("WA Init Error (sqlstore): %v", err)
		return
	}
	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		log.Printf("WA Device Store Not Found, creating new: %v", err)
		deviceStore = container.NewDevice()
	}
	if deviceStore == nil {
		log.Printf("WA Error: deviceStore is nil")
		return
	}
	waClient = whatsmeow.NewClient(deviceStore, clientLog)
	if waClient == nil {
		log.Printf("WA Error: failed to create whatsmeow client")
		return
	}
	// 명시적으로 히스토리 동기화 다운로드를 활성화 (기본값은 true이나 확인 차원)
	waClient.ManualHistorySyncDownload = false
	waClient.AddEventHandler(handleWhatsAppEvent)

	if waClient.Store.ID != nil {
		log.Printf("WA: Found existing session ID, connecting...")
		if err := waClient.Connect(); err != nil {
			log.Printf("WA Connect Error: %v", err)
		} else {
			log.Printf("WA: Connected successfully")
			waClient.SendPresence(context.Background(), types.PresenceAvailable)
		}
	} else {
		log.Printf("WA: No existing session found, please scan QR code.")
	}
}

func handleWhatsAppEvent(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		text := getMessageText(v.Message)
		sender := v.Info.Sender.User
		if v.Info.PushName != "" {
			sender = v.Info.PushName
		}
		jid := v.Info.Chat
		var interactedUser string
		ci := getContextInfo(v.Message)
		if ci != nil {
			participant := ci.GetParticipant()
			if participant != "" {
				pj, _ := types.ParseJID(participant)
				contact, _ := waClient.Store.Contacts.GetContact(context.Background(), pj)
				if contact.PushName != "" {
					interactedUser = contact.PushName
				} else if contact.FullName != "" {
					interactedUser = contact.FullName
				} else {
					interactedUser = pj.User
				}
			}
		}

		if text != "" {
			log.Printf("[WA-EVENT] Message from %s (Chat: %s, To: %s): %s", sender, jid, interactedUser, text)
			waBufferMu.Lock()
			waMessageBuffer[jid] = append(waMessageBuffer[jid], RawChatMessage{
				User:           sender,
				InteractedUser: interactedUser,
				Text:           text,
				Timestamp:      v.Info.Timestamp,
				RawTS:          v.Info.ID,
			})
			if len(waMessageBuffer[jid]) > 200 {
				waMessageBuffer[jid] = waMessageBuffer[jid][1:]
			}
			waBufferMu.Unlock()
		} else {
			log.Printf("[WA-EVENT] Empty or non-text message received from %s (Chat: %s)", sender, jid)
		}
	case *events.HistorySync:
		log.Printf("[WA-EVENT] History sync received (Type: %s)", v.Data.SyncType)
		addedCount := 0
		for _, conv := range v.Data.Conversations {
			jid, err := types.ParseJID(conv.GetID())
			if err != nil {
				continue
			}

			// Get messages from each conversation within the last 24 hours
			histMsgs := conv.GetMessages()
			threshold := time.Now().Add(-24 * time.Hour).Unix()

			waBufferMu.Lock()
			countPerConv := 0
			for i := len(histMsgs) - 1; i >= 0; i-- {
				hsm := histMsgs[i]
				m := hsm.GetMessage()
				if m == nil {
					continue
				}

				msgTS := int64(m.GetMessageTimestamp())
				if msgTS < threshold {
					// Messages are usually ordered, so we can break early if we reach old messages
					break
				}
				
				// Limit to 100 messages per conversation to avoid memory spikes
				if countPerConv >= 100 {
					break
				}

				text := getMessageText(m.GetMessage())
				if text == "" {
					continue
				}

				sender := "History"
				if m.PushName != nil {
					sender = *m.PushName
				} else if m.GetKey().GetFromMe() {
					sender = "Me"
				}
				
				ts := time.Unix(msgTS, 0)
				rawTS := m.GetKey().GetID()

				var interactedUser string
				ci := getContextInfo(m.GetMessage())
				if ci != nil {
					participant := ci.GetParticipant()
					if participant != "" {
						pj, _ := types.ParseJID(participant)
						contact, _ := waClient.Store.Contacts.GetContact(context.Background(), pj)
						if contact.PushName != "" {
							interactedUser = contact.PushName
						} else if contact.FullName != "" {
							interactedUser = contact.FullName
						} else {
							interactedUser = pj.User
						}
					}
				}

				waMessageBuffer[jid] = append(waMessageBuffer[jid], RawChatMessage{
					User:           sender,
					InteractedUser: interactedUser,
					Text:           text,
					Timestamp:      ts,
					RawTS:          rawTS,
				})
				addedCount++
				countPerConv++
			}
			// Limit buffer size to 200
			if len(waMessageBuffer[jid]) > 200 {
				waMessageBuffer[jid] = waMessageBuffer[jid][len(waMessageBuffer[jid])-200:]
			}
			waBufferMu.Unlock()
		}
		log.Printf("[WA-EVENT] Added %d historical messages to buffer", addedCount)
	case *events.Connected:
		log.Printf("[WA-EVENT] Connected to WhatsApp")
	case *events.OfflineSyncCompleted:
		log.Printf("[WA-EVENT] Offline sync completed")
	case *events.LoggedOut:
		log.Printf("[WA-EVENT] Logged out from WhatsApp")
	default:
		// Log the type of other events to see what we are missing
		log.Printf("[WA-DEBUG-EVENT] Received event type: %T | Detail: %+v", v, v)
	}
}

func GetWhatsAppGroups() ([]*types.GroupInfo, error) {
	if waClient == nil || !waClient.IsLoggedIn() {
		return nil, fmt.Errorf("WA not logged in")
	}
	return waClient.GetJoinedGroups(context.Background())
}

func GetWhatsAppStatus() string {
	if waClient == nil {
		return "uninitialized"
	}
	if !waClient.IsConnected() {
		return "disconnected"
	}
	if !waClient.IsLoggedIn() {
		return "logged_out"
	}
	return "connected"
}

func GetWhatsAppQR(ctx context.Context) (string, error) {
	log.Printf("[WA-TRACE] GetWhatsAppQR started")
	if waClient == nil {
		return "", fmt.Errorf("client not initialized")
	}

	waLoginMu.Lock()
	defer waLoginMu.Unlock()

	if waClient.IsLoggedIn() {
		return "", fmt.Errorf("already logged in")
	}

	// If we already have a fresh QR, return it
	if waLatestQR != "" {
		log.Printf("[WA-TRACE] Returning cached QR code")
		return waLatestQR, nil
	}

	log.Printf("[WA-TRACE] Starting fresh QR generation")
	qrCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)

	qrChan, err := waClient.GetQRChannel(qrCtx)
	if err != nil {
		log.Printf("[WA-TRACE] GetQRChannel initial error: %v", err)
	}

	if !waClient.IsConnected() {
		log.Printf("[WA-TRACE] Connecting client...")
		if err := waClient.Connect(); err != nil {
			cancel()
			return "", fmt.Errorf("failed to connect: %v", err)
		}
		if qrChan == nil {
			qrChan, err = waClient.GetQRChannel(qrCtx)
			if err != nil {
				cancel()
				return "", fmt.Errorf("failed to get QR after connect: %v", err)
			}
		}
	}

	// Start background QR runner to handle expiry and success
	firstQRReceived := make(chan bool, 1)
	go func() {
		defer cancel()
		log.Printf("[WA-TRACE] QR Runner started")
		for {
			select {
			case <-qrCtx.Done():
				log.Printf("[WA-TRACE] QR Runner context done")
				waLatestQR = ""
				return
			case evt, ok := <-qrChan:
				if !ok {
					log.Printf("[WA-TRACE] QR Channel closed")
					waLatestQR = ""
					return
				}
				if evt.Event == "code" {
					log.Printf("[WA-TRACE] New QR Code received (len: %d)", len(evt.Code))
					png, err := qrcode.Encode(evt.Code, qrcode.Medium, 256)
					if err == nil {
						waLatestQR = base64.StdEncoding.EncodeToString(png)
						select {
						case firstQRReceived <- true:
						default:
						}
					}
				} else if evt.Event == "success" {
					log.Printf("[WA-TRACE] WhatsApp Login Successful!")
					waLatestQR = ""
					return
				} else {
					log.Printf("[WA-TRACE] QR Event: %s", evt.Event)
				}
			}
		}
	}()

	// Wait for the first QR code with a shorter timeout for the HTTP response
	select {
	case <-firstQRReceived:
		log.Printf("[WA-TRACE] First QR yielded to caller")
		return waLatestQR, nil
	case <-time.After(30 * time.Second):
		return "", fmt.Errorf("QR generation timeout")
	}
}

func getMessageText(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	if msg.Conversation != nil {
		return *msg.Conversation
	} else if msg.ExtendedTextMessage != nil {
		return *msg.ExtendedTextMessage.Text
	} else if msg.GetConversation() != "" {
		return msg.GetConversation()
	}
	// 이미지나 비디오 캡션도 텍스트로 처리
	if msg.ImageMessage != nil && msg.ImageMessage.Caption != nil {
		return *msg.ImageMessage.Caption
	}
	if msg.VideoMessage != nil && msg.VideoMessage.Caption != nil {
		return *msg.VideoMessage.Caption
	}
	return ""
}

func getContextInfo(msg *waE2E.Message) *waE2E.ContextInfo {
	if msg == nil {
		return nil
	}
	if msg.ExtendedTextMessage != nil {
		return msg.ExtendedTextMessage.ContextInfo
	}
	if msg.ImageMessage != nil {
		return msg.ImageMessage.ContextInfo
	}
	if msg.VideoMessage != nil {
		return msg.VideoMessage.ContextInfo
	}
	if msg.AudioMessage != nil {
		return msg.AudioMessage.ContextInfo
	}
	if msg.DocumentMessage != nil {
		return msg.DocumentMessage.ContextInfo
	}
	if msg.StickerMessage != nil {
		return msg.StickerMessage.ContextInfo
	}
	return nil
}
func GetGroupName(jid types.JID) string {
	if waClient == nil {
		return jid.String()
	}
	contact, err := waClient.Store.Contacts.GetContact(context.Background(), jid)
	if err == nil && (contact.FullName != "" || contact.PushName != "") {
		if contact.FullName != "" {
			return contact.FullName
		}
		return contact.PushName
	}
	// If contact store doesn't have it, try GetGroupInfo for group JIDs
	if jid.Server == "g.us" {
		info, err := waClient.GetGroupInfo(context.Background(), jid)
		if err == nil && info.Name != "" {
			return info.Name
		}
	}
	return jid.User
}
