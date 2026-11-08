package postgres

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/kart-io/goagent/memory"
)

const cursorVersion = 2
const cursorTTL = 20 * time.Minute

type listCursor struct {
	Version  int       `json:"v"`
	Kind     string    `json:"k"`
	Binding  string    `json:"b"`
	Cutoff   int64     `json:"h"`
	Sequence int64     `json:"s,omitempty"`
	Updated  time.Time `json:"u"`
	ID       string    `json:"i"`
	Expires  int64     `json:"e"`
}

func cursorBinding(access memory.MemoryAccess, filter memory.FactFilter) (string, error) {
	scopes := append([]memory.MemoryScope(nil), filter.Scopes...)
	sort.Slice(scopes, func(i, j int) bool { return scopeKey(scopes[i]) < scopeKey(scopes[j]) })
	tags := sortedUnique(filter.Tags)
	sources := sortedUnique(filter.SourceIDs)
	statuses := make([]memory.FactStatus, 0, len(filter.Statuses))
	statuses = append(statuses, filter.Statuses...)
	sort.Slice(statuses, func(i, j int) bool { return statuses[i] < statuses[j] })
	recallClasses := append([]memory.RecallClass(nil), filter.RecallClasses...)
	sort.Slice(recallClasses, func(i, j int) bool { return recallClasses[i] < recallClasses[j] })
	data, err := json.Marshal(struct {
		Version             int
		Tenant, User, Agent string
		Scopes              []memory.MemoryScope
		Statuses            []memory.FactStatus
		RecallClasses       []memory.RecallClass
		Tags, Sources       []string
		After, Before       time.Time
	}{cursorVersion, access.Principal.TenantID, access.Principal.UserID, access.Principal.AgentID, scopes, statuses, recallClasses, tags, sources, filter.UpdatedAfter.UTC(), filter.UpdatedBefore.UTC()})
	if err != nil {
		return "", err
	}
	return digestBinding(data), nil
}

func changeCursorBinding(access memory.MemoryAccess, scope memory.MemoryScope, id memory.FactID) (string, error) {
	data, err := json.Marshal(struct {
		Version             int
		Tenant, User, Agent string
		Scope               memory.MemoryScope
		FactID              memory.FactID
	}{cursorVersion, access.Principal.TenantID, access.Principal.UserID, access.Principal.AgentID, scope, id})
	if err != nil {
		return "", err
	}
	return digestBinding(data), nil
}

func maintenanceCursorBinding(access memory.MaintenanceAccess, filter memory.MaintenanceFilter) (string, error) {
	scopes := append([]memory.MemoryScope(nil), filter.Scopes...)
	sort.Slice(scopes, func(i, j int) bool { return scopeKey(scopes[i]) < scopeKey(scopes[j]) })
	data, err := json.Marshal(struct {
		Version          int
		Tenant, Operator string
		Scopes           []memory.MemoryScope
		States           []string
	}{cursorVersion, access.TenantID, access.OperatorID, scopes, sortedUnique(filter.States)})
	if err != nil {
		return "", err
	}
	return digestBinding(data), nil
}

func digestBinding(data []byte) string {
	sum := sha256.Sum256(data)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func scopeKey(scope memory.MemoryScope) string {
	data, _ := json.Marshal(scope)
	sum := sha256.Sum256(data)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func sortedUnique(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	if len(result) < 2 {
		return result
	}
	out := result[:1]
	for _, value := range result[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

func encodeCursor(secret []byte, cursor listCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func decodeCursor(secret []byte, token, binding string, now time.Time) (listCursor, error) {
	invalid := func() (listCursor, error) {
		return listCursor{}, &memory.MemoryError{Kind: memory.ErrInvalidCursor, Op: "列表游标无效或已过期"}
	}
	if len(token) == 0 || len(token) > 8192 {
		return invalid()
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return invalid()
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return invalid()
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return invalid()
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return invalid()
	}
	var cursor listCursor
	if err = json.Unmarshal(payload, &cursor); err != nil || cursor.Version != cursorVersion || cursor.Binding != binding || cursor.Expires < now.Unix() {
		return invalid()
	}
	return cursor, nil
}
