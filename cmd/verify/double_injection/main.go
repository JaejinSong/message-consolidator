package main

import (
	"fmt"
	"log"
	"message-consolidator/auth"
	"net/http"
	"net/http/httptest"
	"os"
)

func main() {
	// 1. Setup Environment
	os.Setenv("DEFAULT_USER_EMAIL", "test-user@whatap.io")
	auth.AuthDisabled = true

	// 2. Define Test Cases
	testCases := []struct {
		name          string
		initialQuery  string
		expectedCount int
	}{
		{
			name:          "No initial email parameter",
			initialQuery:  "",
			expectedCount: 1,
		},
		{
			name:          "Initial email parameter exists (Already Injected)",
			initialQuery:  "email=test-user@whatap.io",
			expectedCount: 1,
		},
		{
			name:          "Other parameters exist",
			initialQuery:  "lang=ko&status=done",
			expectedCount: 1,
		},
	}

	for _, tc := range testCases {
		fmt.Printf("Run Test: %s\n", tc.name)
		
		// Create a request with initial query
		req := httptest.NewRequest("GET", "/api/user/info?"+tc.initialQuery, nil)
		rr := httptest.NewRecorder()

		// Final handler to check the injected query
		finalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			emails := q["email"]
			
			if len(emails) != tc.expectedCount {
				log.Fatalf("[FAIL] %s: Expected %d email parameter(s), got %d: %v", tc.name, tc.expectedCount, len(emails), emails)
			}
			
			if len(emails) > 0 && emails[0] != "test-user@whatap.io" {
				log.Fatalf("[FAIL] %s: Expected email 'test-user@whatap.io', got '%s'", tc.name, emails[0])
			}
			
			w.WriteHeader(http.StatusOK)
		})

		// Run through AuthMiddleware
		handler := auth.AuthMiddleware(finalHandler)
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			log.Fatalf("[FAIL] %s: Handler returned non-OK status: %d", tc.name, rr.Code)
		}
		
		fmt.Printf("[PASS] %s: Parameter injection verified (Count: %d)\n", tc.name, tc.expectedCount)
	}

	fmt.Println("\n[SUCCESS] All multi-injection regression tests passed.")
}
