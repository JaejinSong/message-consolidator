package store

import (
	"context"
	"fmt"
	"message-consolidator/db"
)

func InsertWAMessage(ctx context.Context, arg db.InsertWAMessageParams) error {
	conn := GetDB()
	if conn == nil {
		return fmt.Errorf("wa_messages: db not initialised")
	}
	return db.New(conn).InsertWAMessage(ctx, arg)
}

// ListWAMessagesParams holds optional filter values for ListWAMessages.
// Zero values mean "no filter": empty string = any text, 0 = any timestamp.
type ListWAMessagesParams struct {
	Email     string
	ChatJID   string
	Direction string
	FromTs    int64
	ToTs      int64
	Limit     int64
	Offset    int64
}

func ListWAMessages(ctx context.Context, p ListWAMessagesParams) ([]db.WaMessage, error) {
	conn := GetDB()
	if conn == nil {
		return nil, fmt.Errorf("wa_messages: db not initialised")
	}
	if p.Limit <= 0 || p.Limit > 200 {
		p.Limit = 50
	}
	return db.New(conn).ListWAMessages(ctx, db.ListWAMessagesParams{
		Column1: p.Email,
		Column2: p.ChatJID,
		Column3: p.Direction,
		Column4: p.FromTs,
		Column5: p.ToTs,
		Limit:   p.Limit,
		Offset:  p.Offset,
	})
}
