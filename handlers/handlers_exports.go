package handlers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"message-consolidator/auth"
	"message-consolidator/logger"
	"message-consolidator/store"
	"net/http"
	"time"

	"github.com/xuri/excelize/v2"
)

func HandleExportExcel(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	q := r.URL.Query().Get("q")

	msgs, _, err := store.GetArchivedMessagesFiltered(email, 10000, 0, q, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	f := excelize.NewFile()
	defer f.Close()

	sheet := "Tasks"
	index, _ := f.NewSheet(sheet)
	f.SetActiveSheet(index)
	f.DeleteSheet("Sheet1")

	headers := []string{"ID", "Source", "Room", "Task", "Requester", "Assignee", "Assigned At", "Created At", "Completed At"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, h)
	}

	style, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#E0E0E0"}, Pattern: 1},
	})
	f.SetRowStyle(sheet, 1, 1, style)

	for i, m := range msgs {
		row := i + 2
		compAt := ""
		if m.CompletedAt != nil {
			compAt = m.CompletedAt.Format("2006-01-02 15:04:05")
		}

		f.SetCellValue(sheet, fmt.Sprintf("A%d", row), m.ID)
		f.SetCellValue(sheet, fmt.Sprintf("B%d", row), m.Source)
		f.SetCellValue(sheet, fmt.Sprintf("C%d", row), m.Room)
		f.SetCellValue(sheet, fmt.Sprintf("D%d", row), m.Task)
		f.SetCellValue(sheet, fmt.Sprintf("E%d", row), m.Requester)
		f.SetCellValue(sheet, fmt.Sprintf("F%d", row), m.Assignee)
		f.SetCellValue(sheet, fmt.Sprintf("G%d", row), m.AssignedAt)
		f.SetCellValue(sheet, fmt.Sprintf("H%d", row), m.CreatedAt.Format("2006-01-02 15:04:05"))
		f.SetCellValue(sheet, fmt.Sprintf("I%d", row), compAt)
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("Message_Archive_%s.xlsx", timestamp)

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition")
	if err := f.Write(w); err != nil {
		logger.Errorf("Failed to write excel: %v", err)
	}
}

func HandleExportArchive(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	q := r.URL.Query().Get("q")

	msgs, _, err := store.GetArchivedMessagesFiltered(email, 10000, 0, q, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("Message_Archive_%s.csv", timestamp)

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition")

	w.Write([]byte("\xEF\xBB\xBF"))
	writer := csv.NewWriter(w)
	defer writer.Flush()

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

func HandleExportJSON(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	q := r.URL.Query().Get("q")

	msgs, _, err := store.GetArchivedMessagesFiltered(email, 10000, 0, q, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("Message_Archive_%s.json", timestamp)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition")

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(msgs); err != nil {
		logger.Errorf("Failed to write json export: %v", err)
	}
}
