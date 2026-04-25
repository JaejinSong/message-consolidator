// Package ids defines phantom types for primary-key identifiers.
//
// Phantom types prevent silently mixing IDs of different domain entities
// (e.g. passing a ReportID where a MessageID is expected). Conversions to
// the underlying int64 are required only at the sqlc boundary, where
// generated code in db/ takes/returns plain int64.
package ids

// MessageID identifies a row in consolidated_messages. Also serves as TaskID
// since tasks are persisted as consolidated messages.
type MessageID int64

// ContactID identifies a row in contacts.
type ContactID int64

// ReportID identifies a row in reports.
type ReportID int64

// UserID identifies a row in users.
type UserID int64
