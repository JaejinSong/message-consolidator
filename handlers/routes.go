package handlers

import (
	"message-consolidator/auth"
	"message-consolidator/channels"
	"net/http"
	"os"

	"github.com/gorilla/mux"
)

// RegisterRoutes registers all the API and Auth routes.
func (a *API) RegisterRoutes(r *mux.Router) {
	//Why: Defines authentication-related routes including Google Login, Callback, and Logout.
	r.HandleFunc("/auth/login", auth.HandleGoogleLogin).Methods("GET")
	r.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		auth.HandleGoogleCallback(w, r, a.Config.SlackToken, func(email string) (string, string, error) {
			sc := channels.NewSlackClient(a.Config.SlackToken)
			slackUser, err := sc.LookupUserByEmail(email)
			if err != nil {
				return "", "", err
			}
			return slackUser.ID, slackUser.RealName, nil
		})
	}).Methods("GET")
	r.HandleFunc("/auth/logout", auth.HandleLogout).Methods("GET")

	//Why: Configures protected access to static assets and the main single-page application entry point (index.html), ensuring only authenticated users can access the UI.
	//This logic is bypassed if DISABLE_STATIC_SERVING=true, as external web servers (e.g. Caddy/Nginx) will handle static assets.
	if os.Getenv("DISABLE_STATIC_SERVING") != "true" {
		fs := http.FileServer(http.Dir("./static"))
		r.PathPrefix("/static/").Handler(auth.AuthMiddleware(http.StripPrefix("/static/", fs)))
		r.Handle("/", auth.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				http.ServeFile(w, r, "./index.html")
				return
			}
			fs.ServeHTTP(w, r)
		})))
	}

	//Why: Sets up protected API routes for task management, channel integration, and user profile data, all wrapped in authentication middleware.
	r.Handle("/api/messages", auth.AuthMiddleware(http.HandlerFunc(a.HandleGetMessages))).Methods("GET")
	r.Handle("/api/messages/done", auth.AuthMiddleware(http.HandlerFunc(a.HandleMarkDone))).Methods("POST")
	r.Handle("/api/messages/delete", auth.AuthMiddleware(http.HandlerFunc(a.HandleDelete))).Methods("POST")
	r.Handle("/api/messages/hard-delete", auth.AuthMiddleware(http.HandlerFunc(a.HandleHardDelete))).Methods("POST")
	r.Handle("/api/messages/restore", auth.AuthMiddleware(http.HandlerFunc(a.HandleRestore))).Methods("POST")
	r.Handle("/api/messages/archive", auth.AuthMiddleware(http.HandlerFunc(a.HandleGetArchived))).Methods("GET")
	r.Handle("/api/messages/archive/count", auth.AuthMiddleware(http.HandlerFunc(a.HandleGetArchivedCount))).Methods("GET")
	r.Handle("/api/messages/export", auth.AuthMiddleware(http.HandlerFunc(a.HandleExportArchive))).Methods("GET")
	r.Handle("/api/messages/export/excel", auth.AuthMiddleware(http.HandlerFunc(a.HandleExportExcel))).Methods("GET")
	r.Handle("/api/messages/export/json", auth.AuthMiddleware(http.HandlerFunc(a.HandleExportJSON))).Methods("GET")
	r.Handle("/api/messages/update", auth.AuthMiddleware(http.HandlerFunc(a.HandleUpdateTask))).Methods("POST")
	r.Handle("/api/messages/{id:[0-9]+}/original", auth.AuthMiddleware(http.HandlerFunc(a.HandleGetOriginal))).Methods("GET")
	r.Handle("/api/tasks/translate-batch", auth.AuthMiddleware(http.HandlerFunc(a.HandleTranslateBatchTasks))).Methods("POST")
	r.Handle("/api/tasks/merge", auth.AuthMiddleware(http.HandlerFunc(a.HandleMergeTasks))).Methods("PUT")
	r.Handle("/api/user/info", auth.AuthMiddleware(http.HandlerFunc(a.HandleUserInfo))).Methods("GET")
	r.Handle("/api/whatsapp/qr", auth.AuthMiddleware(http.HandlerFunc(a.HandleWhatsAppQR))).Methods("GET")
	r.Handle("/api/whatsapp/status", auth.AuthMiddleware(http.HandlerFunc(a.HandleWhatsAppStatus))).Methods("GET")
	r.Handle("/api/whatsapp/logout", auth.AuthMiddleware(http.HandlerFunc(a.HandleWhatsAppLogout))).Methods("POST")
	r.Handle("/api/slack/status", auth.AuthMiddleware(http.HandlerFunc(a.HandleSlackStatus))).Methods("GET")
	r.Handle("/api/scan", auth.AuthMiddleware(http.HandlerFunc(a.HandleManualScan))).Methods("GET")
	r.HandleFunc("/api/internal/scan", a.HandleInternalScan).Methods("GET")
	r.Handle("/api/translate", auth.AuthMiddleware(http.HandlerFunc(a.HandleTranslate))).Methods("POST")
	r.Handle("/api/user/aliases", auth.AuthMiddleware(http.HandlerFunc(a.HandleGetUserAliases))).Methods("GET")
	r.Handle("/api/user/alias/add", auth.AuthMiddleware(http.HandlerFunc(a.HandleAddAlias))).Methods("POST")
	r.Handle("/api/user/alias/delete", auth.AuthMiddleware(http.HandlerFunc(a.HandleDeleteAlias))).Methods("POST")
	r.Handle("/api/achievements", auth.AuthMiddleware(http.HandlerFunc(a.HandleGetAchievements))).Methods("GET")
	r.Handle("/api/user/achievements", auth.AuthMiddleware(http.HandlerFunc(a.HandleGetUserAchievements))).Methods("GET")
	r.Handle("/api/user/buy-freeze", auth.AuthMiddleware(http.HandlerFunc(a.HandleBuyStreakFreeze))).Methods("POST")
	r.Handle("/api/user/stats", auth.AuthMiddleware(http.HandlerFunc(a.HandleGetStats))).Methods("GET")
	r.Handle("/api/tenant/aliases", auth.AuthMiddleware(http.HandlerFunc(a.HandleGetTenantAliases))).Methods("GET")
	r.Handle("/api/tenant/alias/add", auth.AuthMiddleware(http.HandlerFunc(a.HandleAddTenantAlias))).Methods("POST")
	r.Handle("/api/tenant/alias/delete", auth.AuthMiddleware(http.HandlerFunc(a.HandleDeleteTenantAlias))).Methods("POST")
	r.Handle("/api/user/token-usage", auth.AuthMiddleware(http.HandlerFunc(a.HandleGetTokenUsage))).Methods("GET")
	r.Handle("/api/contacts/mappings", auth.AuthMiddleware(http.HandlerFunc(a.HandleGetMappings))).Methods("GET")
	r.Handle("/api/contacts/mapping/add", auth.AuthMiddleware(http.HandlerFunc(a.HandleAddMapping))).Methods("POST")
	r.Handle("/api/contacts/mapping/delete", auth.AuthMiddleware(http.HandlerFunc(a.HandleDeleteMapping))).Methods("POST")
	r.Handle("/api/contacts/search", auth.AuthMiddleware(http.HandlerFunc(a.HandleSearchContacts))).Methods("GET")
	r.Handle("/api/contacts/link", auth.AuthMiddleware(http.HandlerFunc(a.HandleLinkAccounts))).Methods("POST")
	r.Handle("/api/contacts/unlink", auth.AuthMiddleware(http.HandlerFunc(a.HandleUnlinkAccount))).Methods("POST")
	r.Handle("/api/contacts/links", auth.AuthMiddleware(http.HandlerFunc(a.HandleGetLinks))).Methods("GET")
	r.Handle("/api/admin/reclassify", auth.AuthMiddleware(http.HandlerFunc(a.HandleReclassifyOldData))).Methods("GET")
	r.Handle("/api/admin/reclassify", auth.AuthMiddleware(http.HandlerFunc(a.HandleReclassifyOldData))).Methods("GET")
	r.Handle("/api/admin/restore-gmail-cc", auth.AuthMiddleware(http.HandlerFunc(a.HandleRestoreGmailCC))).Methods("GET")
	r.Handle("/api/release-notes", auth.AuthMiddleware(http.HandlerFunc(a.HandleGetReleaseNotes))).Methods("GET")
	r.Handle("/api/reports", auth.AuthMiddleware(http.HandlerFunc(a.HandleListReports))).Methods("GET")
	r.Handle("/api/reports/history", auth.AuthMiddleware(http.HandlerFunc(a.HandleGetReportHistory))).Methods("GET")
	r.Handle("/api/reports", auth.AuthMiddleware(http.HandlerFunc(a.HandleGenerateReport))).Methods("POST")
	r.Handle("/api/reports/{id:[0-9]+}", auth.AuthMiddleware(http.HandlerFunc(a.HandleGetReportByID))).Methods("GET")
	r.Handle("/api/reports/{id:[0-9]+}", auth.AuthMiddleware(http.HandlerFunc(a.HandleDeleteReport))).Methods("DELETE")
	r.Handle("/api/reports/{id:[0-9]+}/translate", auth.AuthMiddleware(http.HandlerFunc(a.HandleTranslateReport))).Methods("POST")

	//Why: Provides dedicated OAuth flow endpoints for connecting and disconnected Gmail as a message source.
	r.Handle("/auth/gmail/connect", auth.AuthMiddleware(http.HandlerFunc(a.HandleGmailConnect))).Methods("GET")
	r.HandleFunc("/auth/gmail/callback", a.HandleGmailCallback).Methods("GET")
	r.Handle("/api/gmail/status", auth.AuthMiddleware(http.HandlerFunc(a.HandleGmailStatus))).Methods("GET")
	r.Handle("/api/gmail/disconnect", auth.AuthMiddleware(http.HandlerFunc(a.HandleGmailDisconnect))).Methods("POST")

	//Why: Exposes unauthenticated endpoints such as health checks for automated monitoring and load balancer verification.
	r.HandleFunc("/health", a.HandleHealth).Methods("GET")
}
