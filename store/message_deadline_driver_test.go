package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	"message-consolidator/db"
)

type deadlineDriverValue struct {
	Type  string  `json:"type"`
	Value *string `json:"value,omitempty"`
}

type deadlineDriverColumn struct {
	Name     string `json:"name"`
	Decltype string `json:"decltype,omitempty"`
}

func deadlineDriverText(value string) *string { return &value }

func TestDeadlineQueriesWithHTTPDriver(t *testing.T) {
	for _, tc := range []struct {
		name, value, want string
		inferred          bool
	}{
		{"date", "2026-11-23", "2026-11-23", true},
		{"timestamp", "2026-11-23T23:30:00-05:00", "2026-11-23", false},
		{"null", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Requests []struct {
						Stmt struct {
							SQL string `json:"sql"`
						} `json:"stmt"`
					} `json:"requests"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Requests) != 1 {
					t.Errorf("decode request: %v", err)
					return
				}
				query := request.Requests[0].Stmt.SQL
				columns := make([]deadlineDriverColumn, 27)
				values := make([]deadlineDriverValue, 27)
				for i := range columns {
					columns[i].Name = "text"
					values[i] = deadlineDriverValue{Type: "text", Value: deadlineDriverText("")}
				}
				for _, i := range []int{0, 11, 12, 21} {
					values[i] = deadlineDriverValue{Type: "integer", Value: deadlineDriverText("0")}
				}
				for _, i := range []int{7, 13, 14, 15} {
					values[i] = deadlineDriverValue{Type: "null"}
				}
				values[0].Value = deadlineDriverText("1")
				values[26].Value = deadlineDriverText("[]")
				if strings.Contains(query, "FROM v_messages") {
					for i := 0; i < 4; i++ {
						columns = append(columns, deadlineDriverColumn{Name: "text"})
						values = append(values, deadlineDriverValue{Type: "text", Value: deadlineDriverText("")})
					}
				}
				day := tc.value
				column := deadlineDriverColumn{Name: "deadline_date", Decltype: "DATE"}
				value := deadlineDriverValue{Type: "text", Value: deadlineDriverText(day)}
				if strings.Contains(query, " AS deadline_day") {
					column = deadlineDriverColumn{Name: "deadline_day", Decltype: "TEXT"}
					if len(day) > 10 {
						value.Value = deadlineDriverText(day[:10])
					}
				} else if day == "" {
					value = deadlineDriverValue{Type: "null"}
				}
				columns = append(columns, column, deadlineDriverColumn{Name: "deadline_inferred"})
				inferred := "0"
				if tc.inferred {
					inferred = "1"
				}
				values = append(values, value, deadlineDriverValue{Type: "integer", Value: deadlineDriverText(inferred)})
				response := struct {
					Results []struct {
						Type     string `json:"type"`
						Response struct {
							Type   string `json:"type"`
							Result struct {
								Cols []deadlineDriverColumn  `json:"cols"`
								Rows [][]deadlineDriverValue `json:"rows"`
							} `json:"result"`
						} `json:"response"`
					} `json:"results"`
				}{}
				response.Results = append(response.Results, struct {
					Type     string `json:"type"`
					Response struct {
						Type   string `json:"type"`
						Result struct {
							Cols []deadlineDriverColumn  `json:"cols"`
							Rows [][]deadlineDriverValue `json:"rows"`
						} `json:"result"`
					} `json:"response"`
				}{})
				response.Results[0].Type = "ok"
				response.Results[0].Response.Type = "execute"
				response.Results[0].Response.Result.Cols = columns
				response.Results[0].Response.Result.Rows = [][]deadlineDriverValue{values}
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Errorf("encode response: %v", err)
				}
			}))
			defer server.Close()
			conn, err := sql.Open("libsql", server.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			queries := db.New(conn)
			row, err := queries.GetMessageByID(context.Background(), 1)
			if err != nil {
				t.Errorf("GetMessageByID: %v", err)
			} else {
				message := toConsolidatedFromByID(row)
				if message.DeadlineDate != tc.want || message.DeadlineInferred != tc.inferred {
					t.Errorf("GetMessageByID: got %q, %v", message.DeadlineDate, message.DeadlineInferred)
				}
			}
			rows, err := queries.RefreshCacheActive(context.Background(), sql.NullString{String: "user", Valid: true})
			if err != nil {
				t.Errorf("RefreshCacheActive: %v", err)
			} else {
				messages := buildMessages(rows, nil, map[string]bool{})
				if len(messages) != 1 || messages[0].DeadlineDate != tc.want || messages[0].DeadlineInferred != tc.inferred {
					t.Errorf("RefreshCacheActive: got %+v", messages)
				}
			}
		})
	}
}
