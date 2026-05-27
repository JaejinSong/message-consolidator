package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"message-consolidator/db"
	"message-consolidator/store"
)

// waQueryAuth returns a middleware that validates a static Bearer token.
// Why: no OAuth needed — this is a private read-only endpoint for personal tooling.
func waQueryAuth(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" || r.Header.Get("Authorization") != "Bearer "+token {
				respondError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// HandleWASpec serves GET /api/wa/spec — OpenAPI 3.0 spec (no auth required).
func (a *API) HandleWASpec(w http.ResponseWriter, r *http.Request) {
	spec := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":   "WhatsApp Messages API",
			"version": "1.0.0",
		},
		"servers": []map[string]any{
			{"url": a.Config.AppBaseURL},
		},
		"security": []map[string]any{
			{"bearerAuth": []string{}},
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "opaque",
					"description":  "Static token from WA_QUERY_TOKEN env var",
				},
			},
			"schemas": map[string]any{
				"WAMessage": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":             map[string]any{"type": "integer"},
						"message_id":     map[string]any{"type": "string"},
						"email":          map[string]any{"type": "string"},
						"chat_jid":       map[string]any{"type": "string", "description": "Chat room JID. Personal: <number>@s.whatsapp.net, Group: <id>@g.us"},
						"chat_name":      map[string]any{"type": "string", "description": "Human-readable chat room name"},
						"sender":         map[string]any{"type": "string"},
						"direction":      map[string]any{"type": "string", "enum": []string{"incoming", "outgoing"}},
						"body":           map[string]any{"type": "string"},
						"reply_to":       map[string]any{"type": "string"},
						"has_attachment": map[string]any{"type": "integer", "enum": []int{0, 1}},
						"is_forwarded":   map[string]any{"type": "integer", "enum": []int{0, 1}},
						"mentions":       map[string]any{"type": "string", "description": "JSON array of mentioned display names"},
						"ts":             map[string]any{"type": "integer", "description": "Unix timestamp (seconds)"},
						"created_at":     map[string]any{"type": "string"},
					},
				},
			},
		},
		"paths": map[string]any{
			"/api/wa/messages": map[string]any{
				"get": map[string]any{
					"summary":     "List WhatsApp messages",
					"operationId": "listWAMessages",
					"parameters": []map[string]any{
						{"name": "date", "in": "query", "schema": map[string]any{"type": "string", "example": "2026-05-25"}, "description": "Single day (YYYY-MM-DD, Asia/Seoul). Takes precedence over from/to."},
						{"name": "from", "in": "query", "schema": map[string]any{"type": "string", "format": "date-time"}, "description": "Range start (RFC3339)"},
						{"name": "to", "in": "query", "schema": map[string]any{"type": "string", "format": "date-time"}, "description": "Range end (RFC3339)"},
						{"name": "chat_jid", "in": "query", "schema": map[string]any{"type": "string"}, "description": "Filter by chat room JID"},
						{"name": "direction", "in": "query", "schema": map[string]any{"type": "string", "enum": []string{"incoming", "outgoing"}}},
						{"name": "email", "in": "query", "schema": map[string]any{"type": "string"}},
						{"name": "limit", "in": "query", "schema": map[string]any{"type": "integer", "default": 50, "maximum": 200}},
						{"name": "offset", "in": "query", "schema": map[string]any{"type": "integer", "default": 0}},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "OK",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"messages": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/WAMessage"}},
											"count":    map[string]any{"type": "integer"},
											"offset":   map[string]any{"type": "integer"},
										},
									},
								},
							},
						},
						"401": map[string]any{"description": "Unauthorized — missing or invalid Bearer token"},
					},
				},
			},
		},
	}
	respondJSON(w, http.StatusOK, spec)
}

var seoulLoc = func() *time.Location {
	loc, _ := time.LoadLocation("Asia/Seoul")
	return loc
}()

// HandleListWAMessages serves GET /api/wa/messages.
// Query params (all optional): chat_jid, direction, date (YYYY-MM-DD, Asia/Seoul),
// from (RFC3339), to (RFC3339), email, limit, offset.
// date takes precedence over from/to when both are present.
func (a *API) HandleListWAMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit, _ := strconv.ParseInt(q.Get("limit"), 10, 64)
	offset, _ := strconv.ParseInt(q.Get("offset"), 10, 64)

	var fromTs, toTs int64
	if d := q.Get("date"); d != "" {
		if t, err := time.ParseInLocation("2006-01-02", d, seoulLoc); err == nil {
			fromTs = t.Unix()
			toTs = t.Add(24*time.Hour - time.Second).Unix()
		} else {
			respondError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
			return
		}
	} else {
		if s := q.Get("from"); s != "" {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				fromTs = t.Unix()
			}
		}
		if s := q.Get("to"); s != "" {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				toTs = t.Unix()
			}
		}
	}

	direction := strings.ToLower(q.Get("direction"))
	if direction != "incoming" && direction != "outgoing" {
		direction = ""
	}

	msgs, err := store.ListWAMessages(r.Context(), store.ListWAMessagesParams{
		Email:     q.Get("email"),
		ChatJID:   q.Get("chat_jid"),
		Direction: direction,
		FromTs:    fromTs,
		ToTs:      toTs,
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to query messages")
		return
	}
	if msgs == nil {
		msgs = []db.WaMessage{}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"messages": msgs,
		"count":    len(msgs),
		"offset":   offset,
	})
}
