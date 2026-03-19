package main

import (
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

var cfg *Config

// Constants and logging functions moved to logger.go

func main() {
	initLogging()
	cfg = LoadConfig()

	// Initialize DB
	if err := InitDB(cfg.NeonDBURL); err != nil {
		log.Fatalf("DB Init failed: %v", err)
	}

	// Load Metadata into Memory Cache
	if err := LoadMetadata(); err != nil {
		warnf("Failed to load metadata cache: %v", err)
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
	r.Handle("/api/messages/hard-delete", AuthMiddleware(http.HandlerFunc(handleHardDelete))).Methods("POST")
	r.Handle("/api/messages/restore", AuthMiddleware(http.HandlerFunc(handleRestore))).Methods("POST")
	r.Handle("/api/messages/archive", AuthMiddleware(http.HandlerFunc(handleGetArchived))).Methods("GET")
	r.Handle("/api/messages/archive/count", AuthMiddleware(http.HandlerFunc(handleGetArchivedCount))).Methods("GET")
	r.Handle("/api/messages/export", AuthMiddleware(http.HandlerFunc(handleExportArchive))).Methods("GET")
	r.Handle("/api/messages/export/excel", AuthMiddleware(http.HandlerFunc(handleExportExcel))).Methods("GET")
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

	infof("Server starting on :8080...")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}
