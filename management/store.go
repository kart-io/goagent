package management

import (
	"context"
	"github.com/kart-io/goagent/codec/json"
	"time"
)

// Document 隔离存储与业务实体；Kind 只能取 Kinds 列出的固定表名。
type Document struct {
	Kind, TenantID, ID string
	Revision           int64
	CreatedAt          time.Time
	Data               json.RawMessage
}

var Kinds = []string{"agents", "versions", "sessions", "tasks", "attempts", "approvals", "commands", "idempotency", "audit", "outbox", "workers"}

type Selection struct {
	TenantID   string
	Query      Query
	Visibility Visibility
}
type Reader interface {
	Get(context.Context, string, string, string) (Document, error)
	List(context.Context, string, Selection) ([]Document, error)
}
type Tx interface {
	Reader
	Insert(context.Context, Document) error
	CAS(context.Context, Document, int64) error
	Delete(context.Context, string, string, string, int64) error
}
type Store interface {
	Reader
	WithinTx(context.Context, func(Tx) error) error
}
