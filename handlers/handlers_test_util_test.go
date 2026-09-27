package handlers

import (
	"context"
	"message-consolidator/auth"
	"net/http"
)

// WithMockUser returns a context with a mock user email.
func WithMockUser(ctx context.Context, email string) context.Context {
	return context.WithValue(ctx, auth.UserEmailKey, email)
}

// NewMockRequest creates a new http.Request with a mock user in its context.
func NewMockRequest(method, url, email string) *http.Request {
	req, _ := http.NewRequest(method, url, nil)
	return req.WithContext(WithMockUser(req.Context(), email))
}
