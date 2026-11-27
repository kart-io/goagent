package management

import (
	"strings"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
)

// FormatCursor builds the Query.Cursor that resumes a List after the row
// created at createdAt with the given ID: "<RFC3339Nano CreatedAt>|<ID>".
func FormatCursor(createdAt time.Time, id string) string {
	return createdAt.UTC().Format(time.RFC3339Nano) + "|" + id
}

// ParseCursor splits a Query.Cursor built by FormatCursor. A malformed cursor
// returns errors.ErrManagementInvalid.
func ParseCursor(cursor string) (createdAt time.Time, id string, err error) {
	at, id, ok := strings.Cut(cursor, "|")
	if !ok {
		return time.Time{}, "", agentErrors.ErrManagementInvalid
	}
	if createdAt, err = time.Parse(time.RFC3339Nano, at); err != nil {
		return time.Time{}, "", agentErrors.ErrManagementInvalid
	}
	return createdAt, id, nil
}
