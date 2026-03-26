package handlers

import (
	"message-consolidator/auth"
	"message-consolidator/channels"
	"net/http"

	"github.com/gorilla/mux"
)

// RegisterRoutes registers all the API and Auth routes.
func RegisterRoutes(r *mux.Router) {
	// Auth Endpoints
	r.HandleFunc("/auth/login", auth.HandleGoogleLogin).Methods("GET")
	r.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		auth.HandleGoogleCallback(w, r, cfg.SlackToken, func(email string) (string, string, error) {
			sc := channels.NewSlackClient(cfg.SlackToken)
			slackUser, err := sc.LookupUserByEmail(email)
			if err != nil {
				return "", "", err
			}
			return slackUser.ID, slackUser.RealName, nil
		})
	}).Methods("GET")
	r.HandleFunc("/auth/logout", auth.HandleLogout).Methods("GET")

	// Protected Static Files
	fs := http.FileServer(http.Dir("./static"))
	r.PathPrefix("/static/").Handler(auth.AuthMiddleware(http.StripPrefix("/static/", fs)))
	r.Handle("/", auth.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, "./static/index.html")
			return
		}
		fs.ServeHTTP(w, r)
	})))

	// Protected API Endpoints
	r.Handle("/api/messages", auth.AuthMiddleware(http.HandlerFunc(HandleGetMessages))).Methods("GET")
	r.Handle("/api/messages/done", auth.AuthMiddleware(http.HandlerFunc(HandleMarkDone))).Methods("POST")
	r.Handle("/api/messages/delete", auth.AuthMiddleware(http.HandlerFunc(HandleDelete))).Methods("POST")
	r.Handle("/api/messages/hard-delete", auth.AuthMiddleware(http.HandlerFunc(HandleHardDelete))).Methods("POST")
	r.Handle("/api/messages/restore", auth.AuthMiddleware(http.HandlerFunc(HandleRestore))).Methods("POST")
	r.Handle("/api/messages/archive", auth.AuthMiddleware(http.HandlerFunc(HandleGetArchived))).Methods("GET")
	r.Handle("/api/messages/archive/count", auth.AuthMiddleware(http.HandlerFunc(HandleGetArchivedCount))).Methods("GET")
	r.Handle("/api/messages/export", auth.AuthMiddleware(http.HandlerFunc(HandleExportArchive))).Methods("GET")
	r.Handle("/api/messages/export/excel", auth.AuthMiddleware(http.HandlerFunc(HandleExportExcel))).Methods("GET")
	r.Handle("/api/messages/export/json", auth.AuthMiddleware(http.HandlerFunc(HandleExportJSON))).Methods("GET")
	r.Handle("/api/messages/update", auth.AuthMiddleware(http.HandlerFunc(HandleUpdateTask))).Methods("POST")
	r.Handle("/api/messages/{id:[0-9]+}/original", auth.AuthMiddleware(http.HandlerFunc(HandleGetOriginal))).Methods("GET")
	r.Handle("/api/user/info", auth.AuthMiddleware(http.HandlerFunc(HandleUserInfo))).Methods("GET")
	r.Handle("/api/whatsapp/qr", auth.AuthMiddleware(http.HandlerFunc(HandleWhatsAppQR))).Methods("GET")
	r.Handle("/api/whatsapp/status", auth.AuthMiddleware(http.HandlerFunc(HandleWhatsAppStatus))).Methods("GET")
	r.Handle("/api/whatsapp/logout", auth.AuthMiddleware(http.HandlerFunc(HandleWhatsAppLogout))).Methods("POST")
	r.Handle("/api/slack/status", auth.AuthMiddleware(http.HandlerFunc(HandleSlackStatus))).Methods("GET")
	r.Handle("/api/scan", auth.AuthMiddleware(http.HandlerFunc(HandleManualScan))).Methods("GET")
	r.HandleFunc("/api/internal/scan", HandleInternalScan).Methods("GET")
	r.Handle("/api/translate", auth.AuthMiddleware(http.HandlerFunc(HandleTranslate))).Methods("POST")
	r.Handle("/api/user/aliases", auth.AuthMiddleware(http.HandlerFunc(HandleGetUserAliases))).Methods("GET")
	r.Handle("/api/user/alias/add", auth.AuthMiddleware(http.HandlerFunc(HandleAddAlias))).Methods("POST")
	r.Handle("/api/user/alias/delete", auth.AuthMiddleware(http.HandlerFunc(HandleDeleteAlias))).Methods("POST")
	r.Handle("/api/achievements", auth.AuthMiddleware(http.HandlerFunc(HandleGetAchievements))).Methods("GET")
	r.Handle("/api/user/achievements", auth.AuthMiddleware(http.HandlerFunc(HandleGetUserAchievements))).Methods("GET")
	r.Handle("/api/user/buy-freeze", auth.AuthMiddleware(http.HandlerFunc(HandleBuyStreakFreeze))).Methods("POST")
	r.Handle("/api/user/stats", auth.AuthMiddleware(http.HandlerFunc(HandleGetStats))).Methods("GET")
	r.Handle("/api/tenant/aliases", auth.AuthMiddleware(http.HandlerFunc(HandleGetTenantAliases))).Methods("GET")
	r.Handle("/api/tenant/alias/add", auth.AuthMiddleware(http.HandlerFunc(HandleAddTenantAlias))).Methods("POST")
	r.Handle("/api/tenant/alias/delete", auth.AuthMiddleware(http.HandlerFunc(HandleDeleteTenantAlias))).Methods("POST")
	r.Handle("/api/user/token-usage", auth.AuthMiddleware(http.HandlerFunc(HandleGetTokenUsage))).Methods("GET")
	r.Handle("/api/contacts/mappings", auth.AuthMiddleware(http.HandlerFunc(HandleGetMappings))).Methods("GET")
	r.Handle("/api/contacts/mapping/add", auth.AuthMiddleware(http.HandlerFunc(HandleAddMapping))).Methods("POST")
	r.Handle("/api/contacts/mapping/delete", auth.AuthMiddleware(http.HandlerFunc(HandleDeleteMapping))).Methods("POST")
	r.Handle("/api/admin/reclassify", auth.AuthMiddleware(http.HandlerFunc(HandleReclassifyOldData))).Methods("GET")
	r.Handle("/api/admin/restore-gmail-cc", auth.AuthMiddleware(http.HandlerFunc(HandleRestoreGmailCC))).Methods("GET")
	r.Handle("/api/release-notes", auth.AuthMiddleware(http.HandlerFunc(HandleGetReleaseNotes))).Methods("GET")

	// Gmail OAuth Endpoints
	r.Handle("/auth/gmail/connect", auth.AuthMiddleware(http.HandlerFunc(HandleGmailConnect))).Methods("GET")
	r.HandleFunc("/auth/gmail/callback", HandleGmailCallback).Methods("GET")
	r.Handle("/api/gmail/status", auth.AuthMiddleware(http.HandlerFunc(HandleGmailStatus))).Methods("GET")
	r.Handle("/api/gmail/disconnect", auth.AuthMiddleware(http.HandlerFunc(HandleGmailDisconnect))).Methods("POST")

	// Public Endpoints
	r.HandleFunc("/health", HandleHealth).Methods("GET")
}
