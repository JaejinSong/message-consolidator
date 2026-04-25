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

var exportColumns = []string{"ID", "Source", "Room", "Task", "Requester", "Assignee", "Assigned At", "Created At", "Completed At", "Original Message"}

// loadArchiveExport applies the shared (email, q, status) filter used by every export endpoint.
func loadArchiveExport(r *http.Request) ([]store.ConsolidatedMessage, error) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "all"
	}
	filter := store.ArchiveFilter{
		Email:  auth.GetUserEmail(r),
		Query:  r.URL.Query().Get("q"),
		Status: status,
		Limit:  10000,
	}
	msgs, _, err := store.GetArchivedMessagesFiltered(r.Context(), filter)
	return msgs, err
}

func setExportDownloadHeaders(w http.ResponseWriter, contentType, ext string) {
	filename := fmt.Sprintf("Message_Archive_%s.%s", time.Now().Format("20060102_150405"), ext)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition")
}

func formatCompletedAt(m store.ConsolidatedMessage) string {
	if m.CompletedAt == nil {
		return ""
	}
	return m.CompletedAt.Format("2006-01-02 15:04:05")
}

func (a *API) HandleExportExcel(w http.ResponseWriter, r *http.Request) {
	msgs, err := loadArchiveExport(r)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	f := excelize.NewFile()
	defer f.Close()
	writeExcelArchiveSheet(f, msgs)

	setExportDownloadHeaders(w, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "xlsx")
	if err := f.Write(w); err != nil {
		logger.Errorf("Failed to write excel: %v", err)
	}
}

func writeExcelArchiveSheet(f *excelize.File, msgs []store.ConsolidatedMessage) {
	const sheet = "Tasks"
	index, _ := f.NewSheet(sheet)
	f.SetActiveSheet(index)
	f.DeleteSheet("Sheet1")

	for i, h := range exportColumns {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, h)
	}

	// Why: bold + grey fill on the header row makes the export easier to scan.
	style, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#E0E0E0"}, Pattern: 1},
	})
	f.SetRowStyle(sheet, 1, 1, style)

	for i, m := range msgs {
		writeExcelArchiveRow(f, sheet, i+2, m)
	}
}

func writeExcelArchiveRow(f *excelize.File, sheet string, row int, m store.ConsolidatedMessage) {
	values := []interface{}{
		m.ID, m.Source, m.Room, m.Task, m.Requester, m.Assignee,
		m.AssignedAt.Format("2006-01-02 15:04:05"),
		m.CreatedAt.Format("2006-01-02 15:04:05"),
		formatCompletedAt(m),
		m.OriginalText,
	}
	for i, v := range values {
		cell, _ := excelize.CoordinatesToCellName(i+1, row)
		f.SetCellValue(sheet, cell, v)
	}
}

func (a *API) HandleExportArchive(w http.ResponseWriter, r *http.Request) {
	msgs, err := loadArchiveExport(r)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	setExportDownloadHeaders(w, "text/csv; charset=utf-8", "csv")
	w.Write([]byte("\xEF\xBB\xBF"))
	writer := csv.NewWriter(w)
	defer writer.Flush()

	writer.Write(exportColumns)
	for _, m := range msgs {
		writer.Write([]string{
			fmt.Sprintf("%d", m.ID),
			m.Source, m.Room, m.Task, m.Requester, m.Assignee,
			m.AssignedAt.Format("2006-01-02 15:04:05"),
			m.CreatedAt.Format("2006-01-02 15:04:05"),
			formatCompletedAt(m),
			m.OriginalText,
		})
	}
}

func (a *API) HandleExportJSON(w http.ResponseWriter, r *http.Request) {
	msgs, err := loadArchiveExport(r)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	setExportDownloadHeaders(w, "application/json; charset=utf-8", "json")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(msgs); err != nil {
		logger.Errorf("Failed to write json export: %v", err)
	}
}
