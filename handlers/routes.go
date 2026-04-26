package handlers

import (
	"message-consolidator/auth"
	"message-consolidator/channels"
	"net/http"
	"os"

	"github.com/gorilla/mux"
)

// RegisterRoutes registers all API and Auth routes.
func (a *API) RegisterRoutes(r *mux.Router) {
	// Why: Manual WhaTap APM instrumentation — wraps every request as a transaction
	// so HTTP traffic is visible in WhaTap's resp_time/tps_context dashboards.
	// Outermost so auth failures and unauthenticated probes are also traced.
	r.Use(WhatapMiddleware)

	a.registerAuthRoutes(r)
	a.registerStaticRoutes(r)
	a.registerMessageRoutes(r)
	a.registerChannelRoutes(r)
	a.registerUserRoutes(r)
	a.registerContactRoutes(r)
	a.registerIdentityRoutes(r)
	a.registerAdminRoutes(r)
	a.registerReportRoutes(r)
	a.registerGmailRoutes(r)
	r.HandleFunc("/health", a.HandleHealth).Methods("GET")
}

// protected wraps an API handler in the auth middleware. Returns the wrapped handler.
func (a *API) protected(h http.HandlerFunc) http.Handler {
	return auth.AuthMiddleware(http.HandlerFunc(h))
}

func (a *API) registerAuthRoutes(r *mux.Router) {
	r.HandleFunc("/auth/login", auth.HandleGoogleLogin).Methods("GET")
	r.HandleFunc("/auth/callback", a.handleAuthCallback).Methods("GET")
	r.HandleFunc("/auth/logout", auth.HandleLogout).Methods("GET")
}

func (a *API) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	auth.HandleGoogleCallback(w, r, a.Config.SlackToken, func(email string) (string, string, error) { //nolint:contextcheck // Auth callback uses request ctx through HandleGoogleCallback; SlackClient constructor is ctx-free.
		sc := channels.NewSlackClient(a.Config.SlackToken)
		slackUser, err := sc.LookupUserByEmail(email)
		if err != nil {
			return "", "", err
		}
		return slackUser.ID, slackUser.RealName, nil
	})
}

// registerStaticRoutes serves the SPA + static assets unless DISABLE_STATIC_SERVING=true,
// in which case Caddy/Nginx is expected to handle them externally.
func (a *API) registerStaticRoutes(r *mux.Router) {
	if os.Getenv("DISABLE_STATIC_SERVING") == "true" {
		return
	}
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

func (a *API) registerMessageRoutes(r *mux.Router) {
	r.Handle("/api/messages", a.protected(a.HandleGetMessages)).Methods("GET")
	r.Handle("/api/messages/done", a.protected(a.HandleMarkDone)).Methods("POST")
	r.Handle("/api/messages/delete", a.protected(a.HandleDelete)).Methods("POST")
	r.Handle("/api/messages/hard-delete", a.protected(a.HandleHardDelete)).Methods("POST")
	r.Handle("/api/messages/restore", a.protected(a.HandleRestore)).Methods("POST")
	r.Handle("/api/messages/archive", a.protected(a.HandleGetArchived)).Methods("GET")
	r.Handle("/api/messages/archive/count", a.protected(a.HandleGetArchivedCount)).Methods("GET")
	r.Handle("/api/messages/export", a.protected(a.HandleExportArchive)).Methods("GET")
	r.Handle("/api/messages/export/excel", a.protected(a.HandleExportExcel)).Methods("GET")
	r.Handle("/api/messages/export/json", a.protected(a.HandleExportJSON)).Methods("GET")
	r.Handle("/api/messages/update", a.protected(a.HandleUpdateTask)).Methods("POST")
	r.Handle("/api/messages/{id:[0-9]+}/original", a.protected(a.HandleGetOriginal)).Methods("GET")
	r.Handle("/api/tasks/translate-batch", a.protected(a.HandleTranslateBatchTasks)).Methods("POST")
	r.Handle("/api/tasks/merge", a.protected(a.HandleMergeTasks)).Methods("PUT")
	r.Handle("/api/subtasks/toggle", a.protected(a.HandleToggleSubtask)).Methods("POST")
}

// Channel status string convention: every /api/<channel>/status handler MUST emit
// lowercase status values ("connected" / "disconnected" / channel-specific state).
// Frontend comparison goes through src/utils.ts:isStatusConnected which normalizes case
// as a safety net — but new channels should still adhere to the lowercase standard.
func (a *API) registerChannelRoutes(r *mux.Router) {
	r.Handle("/api/whatsapp/qr", a.protected(a.HandleWhatsAppQR)).Methods("GET")
	r.Handle("/api/whatsapp/status", a.protected(a.HandleWhatsAppStatus)).Methods("GET")
	r.Handle("/api/whatsapp/logout", a.protected(a.HandleWhatsAppLogout)).Methods("POST")
	r.Handle("/api/slack/status", a.protected(a.HandleSlackStatus)).Methods("GET")
	r.Handle("/api/telegram/status", a.protected(a.HandleTelegramStatus)).Methods("GET")
	r.Handle("/api/telegram/auth/start", a.protected(a.HandleTelegramAuthStart)).Methods("POST")
	r.Handle("/api/telegram/auth/confirm", a.protected(a.HandleTelegramAuthConfirm)).Methods("POST")
	r.Handle("/api/telegram/auth/password", a.protected(a.HandleTelegramAuthPassword)).Methods("POST")
	r.Handle("/api/telegram/logout", a.protected(a.HandleTelegramLogout)).Methods("POST")
	r.Handle("/api/telegram/credentials", a.protected(a.HandleTelegramSetCredentials)).Methods("POST")
	r.Handle("/api/scan", a.protected(a.HandleManualScan)).Methods("GET")
	r.HandleFunc("/api/internal/scan", a.HandleInternalScan).Methods("GET")
	r.Handle("/api/translate", a.protected(a.HandleTranslate)).Methods("POST")
}

func (a *API) registerUserRoutes(r *mux.Router) {
	r.Handle("/api/user/info", a.protected(a.HandleUserInfo)).Methods("GET")
	r.Handle("/api/user/aliases", a.protected(a.HandleGetUserAliases)).Methods("GET")
	r.Handle("/api/user/alias/add", a.protected(a.HandleAddAlias)).Methods("POST")
	r.Handle("/api/user/alias/delete", a.protected(a.HandleDeleteAlias)).Methods("POST")
	r.Handle("/api/user/stats", a.protected(a.HandleGetStats)).Methods("GET")
	r.Handle("/api/user/token-usage", a.protected(a.HandleGetTokenUsage)).Methods("GET")
	r.Handle("/api/tenant/aliases", a.protected(a.HandleGetTenantAliases)).Methods("GET")
	r.Handle("/api/tenant/alias/add", a.protected(a.HandleAddTenantAlias)).Methods("POST")
	r.Handle("/api/tenant/alias/delete", a.protected(a.HandleDeleteTenantAlias)).Methods("POST")
	r.Handle("/api/release-notes", a.protected(a.HandleGetReleaseNotes)).Methods("GET")
}

func (a *API) registerContactRoutes(r *mux.Router) {
	r.Handle("/api/contacts/mappings", a.protected(a.HandleGetMappings)).Methods("GET")
	r.Handle("/api/contacts/mapping/add", a.protected(a.HandleAddMapping)).Methods("POST")
	r.Handle("/api/contacts/mapping/delete", a.protected(a.HandleDeleteMapping)).Methods("POST")
	r.Handle("/api/contacts/search", a.protected(a.HandleSearchContacts)).Methods("GET")
	r.Handle("/api/contacts/link", a.protected(a.HandleLinkAccounts)).Methods("POST")
	r.Handle("/api/contacts/unlink", a.protected(a.HandleUnlinkAccount)).Methods("POST")
	r.Handle("/api/contacts/links", a.protected(a.HandleGetLinks)).Methods("GET")
}

func (a *API) registerIdentityRoutes(r *mux.Router) {
	r.Handle("/api/identity/proposals/generate", a.protected(a.HandleGenerateProposals)).Methods("POST")
	r.Handle("/api/identity/proposals/job-status", a.protected(a.HandleProposalJobStatus)).Methods("GET")
	r.Handle("/api/identity/proposals", a.protected(a.HandleListProposals)).Methods("GET")
	r.Handle("/api/identity/proposals/{id}/accept", a.protected(a.HandleAcceptProposal)).Methods("POST")
	r.Handle("/api/identity/proposals/{id}/reject", a.protected(a.HandleRejectProposal)).Methods("POST")
}

func (a *API) registerAdminRoutes(r *mux.Router) {
	r.Handle("/api/admin/reclassify", a.protected(a.HandleReclassifyOldData)).Methods("GET")
	r.Handle("/api/admin/invalidate-cache", a.protected(a.HandleInvalidateCache)).Methods("POST")
	r.Handle("/api/admin/restore-gmail-cc", a.protected(a.HandleRestoreGmailCC)).Methods("GET")
}

func (a *API) registerReportRoutes(r *mux.Router) {
	r.Handle("/api/reports", a.protected(a.HandleListReports)).Methods("GET")
	r.Handle("/api/reports/history", a.protected(a.HandleGetReportHistory)).Methods("GET")
	r.Handle("/api/reports", a.protected(a.HandleGenerateReport)).Methods("POST")
	r.Handle("/api/reports/{id:[0-9]+}", a.protected(a.HandleGetReportByID)).Methods("GET")
	r.Handle("/api/reports/{id:[0-9]+}", a.protected(a.HandleDeleteReport)).Methods("DELETE")
	r.Handle("/api/reports/{id:[0-9]+}/translate", a.protected(a.HandleTranslateReport)).Methods("POST")
	r.Handle("/api/reports/{id:[0-9]+}/export/notion", a.protected(a.HandleExportReportToNotion)).Methods("POST")
}

func (a *API) registerGmailRoutes(r *mux.Router) {
	r.Handle("/auth/gmail/connect", a.protected(a.HandleGmailConnect)).Methods("GET")
	r.HandleFunc("/auth/gmail/callback", a.HandleGmailCallback).Methods("GET")
	r.Handle("/api/gmail/status", a.protected(a.HandleGmailStatus)).Methods("GET")
	r.Handle("/api/gmail/disconnect", a.protected(a.HandleGmailDisconnect)).Methods("POST")
}
