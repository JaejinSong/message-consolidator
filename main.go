package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/slack-go/slack"
	"gopkg.in/natefinch/lumberjack.v2"
)

var cfg *Config

func main() {
	initLogging()
	cfg = LoadConfig()

	// Initialize DB
	if err := InitDB(cfg.NeonDBURL); err != nil {
		log.Fatalf("DB Init failed: %v", err)
	}

	// Initialize WhatsApp for all existing users
	users, _ := GetAllUsers()
	for _, u := range users {
		go InitWhatsApp(u.Email)
	}

	// Initialize OAuth
	SetupOAuth()
	SetupGmailOAuth()

	// Start Background Workers
	go startBackgroundScanner()

	// Create a new router
	r := mux.NewRouter()

	// Auth Endpoints
	r.HandleFunc("/auth/login", handleGoogleLogin).Methods("GET")
	r.HandleFunc("/auth/callback", handleGoogleCallback).Methods("GET")
	r.HandleFunc("/auth/logout", handleLogout).Methods("GET")

	// Protected Static Files
	fs := http.FileServer(http.Dir("./static"))
	r.PathPrefix("/static/").Handler(AuthMiddleware(http.StripPrefix("/static/", fs)))
	r.Handle("/", AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, "./static/index.html")
			return
		}
		fs.ServeHTTP(w, r)
	})))

	// Protected API Endpoints
	r.Handle("/api/messages", AuthMiddleware(http.HandlerFunc(handleGetMessages))).Methods("GET")
	r.Handle("/api/messages/done", AuthMiddleware(http.HandlerFunc(handleMarkDone))).Methods("POST")
	r.Handle("/api/messages/delete", AuthMiddleware(http.HandlerFunc(handleDelete))).Methods("POST")
	r.Handle("/api/messages/archive", AuthMiddleware(http.HandlerFunc(handleGetArchived))).Methods("GET")
	r.Handle("/api/messages/export", AuthMiddleware(http.HandlerFunc(handleExportArchive))).Methods("GET")
	r.Handle("/api/messages/update", AuthMiddleware(http.HandlerFunc(handleUpdateTask))).Methods("POST")
	r.Handle("/api/user/info", AuthMiddleware(http.HandlerFunc(handleUserInfo))).Methods("GET")
	r.Handle("/api/whatsapp/qr", AuthMiddleware(http.HandlerFunc(handleWhatsAppQR))).Methods("GET")
	r.Handle("/api/whatsapp/status", AuthMiddleware(http.HandlerFunc(handleWhatsAppStatus))).Methods("GET")
	r.Handle("/api/scan", AuthMiddleware(http.HandlerFunc(handleManualScan))).Methods("GET")
	r.Handle("/api/translate", AuthMiddleware(http.HandlerFunc(handleTranslate))).Methods("POST")
	r.Handle("/api/user/aliases", AuthMiddleware(http.HandlerFunc(handleGetUserAliases))).Methods("GET")
	r.Handle("/api/user/alias/add", AuthMiddleware(http.HandlerFunc(handleAddAlias))).Methods("POST")
	r.Handle("/api/user/alias/delete", AuthMiddleware(http.HandlerFunc(handleDeleteAlias))).Methods("POST")

	// Gmail OAuth Endpoints
	r.Handle("/auth/gmail/connect", AuthMiddleware(http.HandlerFunc(handleGmailConnect))).Methods("GET")
	r.HandleFunc("/auth/gmail/callback", handleGmailCallback).Methods("GET")
	r.Handle("/api/gmail/status", AuthMiddleware(http.HandlerFunc(handleGmailStatus))).Methods("GET")

	// Attach the router to the default http server
	http.Handle("/", r)

	log.Println("Server starting on :8080...")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}

func handleGetMessages(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	msgs, err := GetMessages(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msgs)
}

func handleMarkDone(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		ID   int  `json:"id"`
		Done bool `json:"done"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := MarkMessageDone(email, req.ID, req.Done); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleGetArchived(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	msgs, err := GetArchivedMessages(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msgs)
}

func handleExportArchive(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	msgs, err := GetArchivedMessages(email)
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
	email := GetUserEmail(r)
	status := GetWhatsAppStatus(email)
	json.NewEncoder(w).Encode(map[string]string{"status": status})
}

func handleManualScan(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "Korean"
	}
	log.Printf("Manual scan triggered via API for %s (lang: %s)", email, lang)
	go scan(email, lang) // Pass email to scan
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "scan started", "lang": lang})
}

func handleWhatsAppQR(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	qr, err := GetWhatsAppQR(r.Context(), email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"qr": qr})
}

func handleTranslate(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "Korean"
	}

	msgs, err := GetMessages(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var reqs []TranslateRequest
	for _, m := range msgs {
		reqs = append(reqs, TranslateRequest{
			ID:           m.ID,
			Text:         m.Task,
			OriginalText: m.OriginalText,
		})
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
		UpdateTaskText(email, t.ID, t.Text)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success", "translated_count": fmt.Sprintf("%d", len(translations))})
}

// New handler for deleting messages
func handleDelete(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := DeleteMessage(email, req.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// New handler for updating task text
func handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		ID   int    `json:"id"`
		Task string `json:"task"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := UpdateTaskText(email, req.ID, req.Task); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// New handler for user info
func handleUserInfo(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	log.Printf("Fetching user info for: %s", email)
	user, err := GetOrCreateUser(email, "", "")
	if err != nil {
		log.Printf("handleUserInfo Error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	aliases, err := GetUserAliases(user.ID)
	if err == nil {
		user.Aliases = aliases
	}

	if len(user.Aliases) == 0 {
		log.Printf("[DEBUG] No aliases found for %s, attempting self-heal with Slack Token: %s...", user.Email, cfg.SlackToken[:10]+"***")
		sc := NewSlackClient(cfg.SlackToken)
		slackUser, err := sc.LookupUserByEmail(user.Email)
		if err != nil {
			log.Printf("[DEBUG] Slack Lookup failed for %s: %v", user.Email, err)
		} else if slackUser != nil {
			log.Printf("[DEBUG] Found Slack User: %s (ID: %s)", slackUser.RealName, slackUser.ID)
			UpdateUserSlackID(user.Email, slackUser.ID)
			AddUserAlias(user.ID, slackUser.RealName)
			if slackUser.Profile.DisplayName != "" {
				AddUserAlias(user.ID, slackUser.Profile.DisplayName)
			}
			user.Aliases, _ = GetUserAliases(user.ID)
			log.Printf("Self-healed aliases for existing user: %s -> %v", user.Email, user.Aliases)
		} else {
			log.Printf("[DEBUG] No Slack user found for %s", user.Email)
		}
	} else {
		log.Printf("[DEBUG] User %s already has aliases: %v", user.Email, user.Aliases)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func handleGetUserAliases(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	user, err := GetOrCreateUser(email, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	aliases, err := GetUserAliases(user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(aliases)
}

func handleAddAlias(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		Alias string `json:"alias"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	user, err := GetOrCreateUser(email, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := AddUserAlias(user.ID, req.Alias); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleDeleteAlias(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		Alias string `json:"alias"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	user, err := GetOrCreateUser(email, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := DeleteUserAlias(user.ID, req.Alias); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func startBackgroundScanner() {
	log.Println("Background scanner started (5m interval)...")
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// Run initial scan
	runAllScans()

	for range ticker.C {
		runAllScans()
	}
}

func runAllScans() {
	users, err := GetAllUsers()
	if err != nil {
		log.Printf("Scanner Error: Failed to get users: %v", err)
		return
	}
	for _, u := range users {
		log.Printf("Starting background scan for: %s", u.Email)
		go scan(u.Email, "Korean") // Default language for background scan
	}
}

func scan(email string, language string) {
	log.Printf("Starting message scan for %s (lang: %s)...", email, language)
	ctx := context.Background()

	// Slack Scan
	log.Printf("[SCAN] About to call scanSlack for %s", email)
	newSlack := scanSlack(ctx, email, language)
	log.Printf("[SCAN] scanSlack finished for %s, hasNew: %v", email, newSlack)

	// WhatsApp Scan
	log.Printf("[SCAN] About to call scanWhatsApp for %s", email)
	newWA := scanWhatsApp(ctx, email, language)
	log.Printf("[SCAN] scanWhatsApp finished for %s, hasNew: %v", email, newWA)

	// Gmail Scan
	log.Printf("[SCAN] About to call ScanGmail for %s", email)
	newGmail := ScanGmail(ctx, email, language)
	log.Printf("[SCAN] ScanGmail finished for %s, hasNew: %v", email, newGmail)

	// Refresh cache only if new messages were actually saved
	if newSlack || newWA || newGmail {
		log.Println("[SCAN] New messages found, refreshing cache...")
		if err := RefreshCache(email); err != nil {
			log.Printf("Error refreshing cache for %s after scan: %v", email, err)
		}
	} else {
		log.Printf("[SCAN] No new messages found for %s, skipping DB cache refresh.", email)
	}
}

func scanSlack(ctx context.Context, email string, language string) bool {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[SCAN-SLACK] PANIC RECOVERED for %s: %v", email, r)
		}
	}()

	log.Printf("TRACELOG: Starting Slack scan for %s...", email)
	hasNew := false
	sc := NewSlackClient(cfg.SlackToken)
	
	// Collect channels to scan
	targetChannels := make(map[string]string) // ID -> Name

	// Discover rooms the bot is a member of with pagination
	cursor := ""
	for {
		params := &slack.GetConversationsParameters{
			Types:  []string{"public_channel", "private_channel"},
			Cursor: cursor,
			Limit:  100,
		}
		
		log.Printf("[SCAN-SLACK] Discovering rooms for %s (cursor: %s)...", email, cursor)
		channels, nextCursor, err := sc.api.GetConversations(params)
		if err != nil {
			log.Printf("[SCAN-SLACK] Error fetching rooms for %s: %v", email, err)
			break
		}

		for _, channel := range channels {
			if channel.IsMember {
				log.Printf("[SCAN-SLACK] Found membership in #%s (%s)", channel.Name, channel.ID)
				targetChannels[channel.ID] = channel.Name
			}
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	log.Printf("[SCAN-SLACK] Total rooms to scan for %s: %d", email, len(targetChannels))

	if len(sc.userMap) == 0 {
		sc.FetchUsers()
	}

	// Scan last 7 days to capture older threads with new activity
	since := time.Now().Add(-7 * 24 * time.Hour)
	for id, name := range targetChannels {
		log.Printf("[SCAN-SLACK] Processing messages from #%s (%s)", name, id)
		msgs, err := sc.GetMessages(id, since)
		if err != nil {
			log.Printf("[SCAN-SLACK] Error getting messages for #%s: %v", name, err)
			continue
		}
		log.Printf("[SCAN-SLACK] Fetched %d messages from #%s", len(msgs), name)
		if len(msgs) == 0 {
			continue
		}

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
		log.Printf("[SCAN-SLACK] Sending %d messages to Gemini for analysis (language: %s)", len(msgs), language)
		items, err := gc.Analyze(ctx, sb.String(), language)
		if err != nil {
			log.Printf("[SCAN-SLACK] Gemini Analyze Error: %v", err)
			continue
		}
		
		itemsJSON, _ := json.Marshal(items)
		log.Printf("[SCAN-SLACK] Gemini Analysis Result for #%s: %s", name, string(itemsJSON))

		for _, item := range items {
			// Extract assignee (Slack user ID to name)
			assignee := ""
			if item.Assignee != "" {
				assignee = sc.userMap[item.Assignee]
				if assignee == "" {
					assignee = item.Assignee
				}
			}

			link := fmt.Sprintf("https://slack.com/app_redirect?channel=%s&message_ts=%s", id, item.SourceTS)

			// assignedAt is now derived from item.SourceTS if available, otherwise current time
			// The original code had a variable `assignedAt` which was then formatted.
			// The instruction implies removing `assignedAt` and directly using `time.Now().Format(...)`
			// for the `AssignedAt` field in `SaveMessage`.
			// However, the provided snippet for `assignedAt` calculation is within an `if item.SourceTS != ""` block,
			// and then the `SaveMessage` call uses `time.Now().Format`.
			// To align with the instruction's implied change and fix the `assignedAt` error,
			// I will remove the `assignedAt` variable and use `time.Now().Format` as shown in the instruction.
			// The `item.SourceTS` based time calculation is removed as per the instruction's diff.

			assignedAt := time.Now().Format(time.RFC3339)
			if ts, ok := msgMap[item.SourceTS]; ok {
				assignedAt = ts.Format(time.RFC3339)
			}

			saved, _ := SaveMessage(ConsolidatedMessage{
				UserEmail:  email,
				Source:     "slack",
				Room:       "#" + name,
				Task:       item.Task,
				Requester:  item.Requester,
				Assignee:   assignee,
				AssignedAt: assignedAt,
				Link:       link,
				SourceTS:   item.SourceTS,
				OriginalText: item.OriginalText,
				Done:       false, 
			})
			if saved {
				hasNew = true
			}
		}
	}
	return hasNew
}

func scanWhatsApp(ctx context.Context, email string, language string) bool {
	log.Printf("[SCAN-WA] Starting WhatsApp scan for %s (Buffer JIDs: %d)", email, len(waMessageBuffer[email])) // Access user-specific buffer
	hasNew := false
	// Assuming waClient is now user-specific or managed to handle multiple users
	// For simplicity, let's assume GetWhatsAppClient(email) returns the client for that user
	userWAClient := GetWhatsAppClient(email)
	if userWAClient == nil || !userWAClient.IsLoggedIn() {
		log.Printf("[SCAN-WA] Skip for %s: Client not initialized or not logged in", email)
		return false
	}

	waBufferMu.RLock()
	defer waBufferMu.RUnlock()

	// Access user-specific message buffer
	userBuffer, ok := waMessageBuffer[email]
	if !ok {
		log.Printf("[SCAN-WA] No WhatsApp message buffer for %s", email)
		return false
	}

	for jid, msgs := range userBuffer {
		if len(msgs) == 0 {
			continue
		}

		groupName := GetGroupName(email, jid)
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
				assignedAt = ts.Format(time.RFC3339)
			} else if sec, err := strconv.ParseInt(assignedAt, 10, 64); err == nil {
				assignedAt = time.Unix(sec, 0).Format(time.RFC3339)
			} else {
				// Fallback if Gemini returns an invalid or partial time (like "05:01")
				assignedAt = time.Now().Format(time.RFC3339)
			}

			saved, _ := SaveMessage(ConsolidatedMessage{
				Source:     "whatsapp",
				Room:       groupName,
				Task:       item.Task,
				Requester:  item.Requester,
				Assignee:   item.Assignee,
				AssignedAt: assignedAt,
				SourceTS:   item.SourceTS,
				OriginalText: item.OriginalText,
			})
			if saved {
				hasNew = true
			}
		}
	}
	return hasNew
}

func initLogging() {
	lumberjackLogger := &lumberjack.Logger{
		Filename:   "logs/app.log",
		MaxSize:    100, // megabytes
		MaxBackups: 30,
		MaxAge:     7,    // days (Requirement 3)
		Compress:   true,
		LocalTime:  true,
	}

	// Double output to console and file (Requirement 1)
	multiWriter := io.MultiWriter(os.Stdout, lumberjackLogger)
	log.SetOutput(multiWriter)

	// Daily rotation logic (Requirement 2)
	go func() {
		for {
			now := time.Now()
			// Calculate time until next midnight
			nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
			time.Sleep(time.Until(nextMidnight))
			
			log.Println("[LOG] Rotating log file for new day...")
			if err := lumberjackLogger.Rotate(); err != nil {
				log.Printf("[LOG] Error rotating log: %v", err)
			}
		}
	}()
}
