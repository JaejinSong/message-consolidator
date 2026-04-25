package handlers

import (
	"net/http"

	"github.com/whatap/go-api/instrumentation/github.com/gorilla/mux/whatapmux"
)

// WhatapMiddleware delegates to the official whatapmux integration.
// Why: Manual instrumentation per WhaTap go-api-example
// (https://github.com/whatap/go-api-example/tree/main/github.com/gorilla/mux).
// `trace.HandlerFunc` underneath captures HTTP method/URL/headers and propagates
// the trace context for downstream `trace.Step` calls in services and store layers.
func WhatapMiddleware(next http.Handler) http.Handler {
	return whatapmux.Middleware()(next)
}
