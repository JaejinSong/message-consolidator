package store

import (
	"embed"
)

//go:embed queries/*.sql migrations/*.sql
var queryFS embed.FS

// SQL struct removal initiated. Legacy bridge is no longer needed.
// Migration functions now directly use sql.DB/sql.Tx Exec calls with strings from embedded FS if necessary,
// or via specific schema functions.
