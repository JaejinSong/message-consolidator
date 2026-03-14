package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

var cfg *Config

func main() {
	cfg = LoadConfig()

	// Initialize DB
	if err := InitDB(cfg.NeonDBURL); err != nil {
		log.Fatalf("DB Init failed: %v", err)
	}

	// Initialize WhatsApp
	InitWhatsApp(context.Background())

	// Initialize OAuth
	SetupOAuth()

	// Start Background Workers
	go startBackgroundScanner()

	// Auth Endpoints
	http.HandleFunc("/auth/login", handleGoogleLogin)
	http.HandleFunc("/auth/callback", handleGoogleCallback)

	// Protected Static Files
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/static/", AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		http.StripPrefix("/static/", fs).ServeHTTP(w, r)
	}))
	http.HandleFunc("/", AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, "./static/index.html")
			return
		}
		fs.ServeHTTP(w, r)
	}))

	// Protected API Endpoints
	http.HandleFunc("/api/messages", AuthMiddleware(handleGetMessages))
	http.HandleFunc("/api/messages/done", AuthMiddleware(handleMarkDone))
	http.HandleFunc("/api/messages/archive", AuthMiddleware(handleGetArchive))
	http.HandleFunc("/api/messages/archive/export", AuthMiddleware(handleExportArchive))
	http.HandleFunc("/api/whatsapp/status", AuthMiddleware(handleWhatsAppStatus))
	http.HandleFunc("/api/whatsapp/qr", AuthMiddleware(handleWhatsAppQR))
	http.HandleFunc("/api/scan", AuthMiddleware(handleManualScan))
	http.HandleFunc("/api/translate", AuthMiddleware(handleTranslate))

	log.Println("Server starting on :8080...")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}

func handleGetMessages(w http.ResponseWriter, r *http.Request) {
	msgs, err := GetMessages()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msgs)
}
	
func handleMarkDone(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID   int  `json:"id"`
		Done bool `json:"done"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := MarkMessageDone(req.ID, req.Done); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleGetArchive(w http.ResponseWriter, r *http.Request) {
	msgs, err := GetArchivedMessages()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(msgs)
}

func handleExportArchive(w http.ResponseWriter, r *http.Request) {
	msgs, err := GetArchivedMessages()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment;filename=tasks_archive.csv")

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Header
	writer.Write([]string{"ID", "Source", "Room", "Task", "Requester", "Assignee", "Assigned At", "Created At", "Completed At"})

	for _, m := range msgs {
		compAt := ""
		if m.CompletedAt != nil {
			compAt = m.CompletedAt.Format("2006-01-02 15:04:05")
		}
		writer.Write([]string{
			fmt.Sprintf("%d", m.ID),
			m.Source,
			m.Room,
			m.Task,
			m.Requester,
			m.Assignee,
			m.AssignedAt,
			m.CreatedAt.Format("2006-01-02 15:04:05"),
			compAt,
		})
	}
}

func handleWhatsAppStatus(w http.ResponseWriter, r *http.Request) {
	status := GetWhatsAppStatus()
	json.NewEncoder(w).Encode(map[string]string{"status": status})
}

func handleManualScan(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "Korean"
	}
	log.Printf("Manual scan triggered via API (lang: %s)", lang)
	go scan(lang)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "scan started", "lang": lang})
}

func handleWhatsAppQR(w http.ResponseWriter, r *http.Request) {
	qr, err := GetWhatsAppQR(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"qr": qr})
}

func handleTranslate(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "Korean"
	}

	msgs, err := GetMessages()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var reqs []TranslateRequest
	for _, m := range msgs {
		reqs = append(reqs, TranslateRequest{ID: m.ID, Text: m.Task})
	}

	gc, err := NewGeminiClient(r.Context(), cfg.GeminiAPIKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	translations, err := gc.Translate(r.Context(), reqs, lang)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, t := range translations {
		UpdateTaskText(t.ID, t.Text)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success", "translated_count": fmt.Sprintf("%d", len(translations))})
}

func startBackgroundScanner() {
	log.Println("Background scanner started (1m interval)...")
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Initial Scan (Default: Korean)
	scan("Korean")

	for range ticker.C {
		scan("Korean")
	}
}

func scan(language string) {
	log.Printf("Starting message scan (lang: %s)...", language)
	ctx := context.Background()

	// Slack Scan
	newSlack := scanSlack(ctx, language)

	// WhatsApp Scan
	newWA := scanWhatsApp(ctx, language)

	// Refresh cache only if new messages were actually saved
	if newSlack || newWA {
		log.Println("[SCAN] New messages found, refreshing cache...")
		if err := RefreshCache(); err != nil {
			log.Printf("Error refreshing cache after scan: %v", err)
		}
	} else {
		log.Println("[SCAN] No new messages found, skipping DB cache refresh.")
	}
}

func scanSlack(ctx context.Context, language string) bool {
	log.Println("[SCAN-SLACK] Starting Slack scan...")
	hasNew := false
	if cfg.SlackToken == "" {
		log.Println("[SCAN-SLACK] Skip: Missing token")
		return false
	}
	sc := NewSlackClient(cfg.SlackToken)
	sc.FetchUsers()

	// ... previous logic to fetch channels ...
	params := &slack.GetConversationsParameters{
		Types: []string{"public_channel", "private_channel"},
	}
	channels, _, err := sc.api.GetConversations(params)
	if err != nil {
		log.Printf("[SCAN-SLACK] Error fetching channels: %v", err)
		return false
	}
	var channelsToScan []slack.Channel
	for _, c := range channels {
		if c.IsMember {
			channelsToScan = append(channelsToScan, c)
		}
	}

	if cfg.SlackChannelID != "" {
		found := false
		for _, c := range channelsToScan {
			if c.ID == cfg.SlackChannelID {
				found = true
				break
			}
		}
		if !found {
			info, err := sc.api.GetConversationInfo(&slack.GetConversationInfoInput{ChannelID: cfg.SlackChannelID})
			if err == nil {
				channelsToScan = append(channelsToScan, *info)
			}
		}
	}

	for _, channel := range channelsToScan {
		msgs, err := sc.GetMessages(channel.ID, time.Now().Add(-24*time.Hour))
		if err != nil {
			continue
		}
		if len(msgs) == 0 {
			continue
		}

		var sb strings.Builder
		for _, m := range msgs {
			toPart := ""
			if m.InteractedUser != "" {
				toPart = fmt.Sprintf(" -> %s", m.InteractedUser)
			}
			sb.WriteString(fmt.Sprintf("[TS:%s] [%s] %s%s: %s\n", m.RawTS, m.Timestamp.Format("15:04"), m.User, toPart, m.Text))
		}

		gc, err := NewGeminiClient(ctx, cfg.GeminiAPIKey)
		if err != nil {
			continue
		}
		items, err := gc.Analyze(ctx, sb.String(), language)
		if err != nil {
			continue
		}

		for _, item := range items {
			link := fmt.Sprintf("https://slack.com/app_redirect?channel=%s&message_ts=%s", channel.ID, item.SourceTS)
			
			assignee := item.Assignee
			if strings.HasPrefix(assignee, "U") || (strings.HasPrefix(assignee, "<@U") && strings.HasSuffix(assignee, ">")) {
				cleanID := strings.TrimPrefix(assignee, "<@")
				cleanID = strings.TrimSuffix(cleanID, ">")
				assignee = sc.GetUserName(cleanID)
			}

			assignedAt := item.AssignedAt
			if item.SourceTS != "" {
				parts := strings.Split(item.SourceTS, ".")
				if sec, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
					t := time.Unix(sec, 0)
					kst := time.FixedZone("KST", 9*60*60)
					assignedAt = t.In(kst).Format("2006-01-02 15:04:05 KST")
				}
			}

			saved, _ := SaveMessage(ConsolidatedMessage{
				Source:     "slack",
				Room:       "#" + channel.Name,
				Task:       item.Task,
				Requester:  item.Requester,
				Assignee:   assignee,
				AssignedAt: assignedAt,
				Link:       link,
				SourceTS:   item.SourceTS,
			})
			if saved {
				hasNew = true
			}
		}
	}
	return hasNew
}

func scanWhatsApp(ctx context.Context, language string) bool {
	log.Printf("[SCAN-WA] Starting WhatsApp scan (Buffer JIDs: %d)", len(waMessageBuffer))
	hasNew := false
	if waClient == nil || !waClient.IsLoggedIn() {
		log.Printf("[SCAN-WA] Skip: Client not initialized or not logged in")
		return false
	}

	waBufferMu.RLock()
	defer waBufferMu.RUnlock()

	for jid, msgs := range waMessageBuffer {
		if len(msgs) == 0 {
			continue
		}
		
		groupName := GetGroupName(jid)
		msgMap := make(map[string]time.Time)
		var sb strings.Builder
		for _, m := range msgs {
			toPart := ""
			if m.InteractedUser != "" {
				toPart = fmt.Sprintf(" -> %s", m.InteractedUser)
			}
			msgMap[m.RawTS] = m.Timestamp
			sb.WriteString(fmt.Sprintf("[TS:%s] [%s] %s%s: %s\n", m.RawTS, m.Timestamp.Format("15:04"), m.User, toPart, m.Text))
		}

		gc, err := NewGeminiClient(ctx, cfg.GeminiAPIKey)
		if err != nil {
			continue
		}
		items, err := gc.Analyze(ctx, sb.String(), language)
		if err != nil {
			continue
		}

		for _, item := range items {
			assignedAt := item.AssignedAt
			if ts, ok := msgMap[item.SourceTS]; ok {
				kst := time.FixedZone("KST", 9*60*60)
				assignedAt = ts.In(kst).Format("2006-01-02 15:04:05 KST")
			} else if sec, err := strconv.ParseInt(assignedAt, 10, 64); err == nil {
				t := time.Unix(sec, 0)
				kst := time.FixedZone("KST", 9*60*60)
				assignedAt = t.In(kst).Format("2006-01-02 15:04:05 KST")
			}

			saved, _ := SaveMessage(ConsolidatedMessage{
				Source:     "whatsapp",
				Room:       groupName,
				Task:       item.Task,
				Requester:  item.Requester,
				Assignee:   item.Assignee,
				AssignedAt: assignedAt,
				SourceTS:   item.SourceTS,
			})
			if saved {
				hasNew = true
			}
		}
	}
	return hasNew
}
