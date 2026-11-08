package management

import "github.com/kart-io/goagent/codec/json"

// resourceReference 统一详情、列表与写操作的父子关系，自身资源也携带所属范围。
func resourceReference(kind, tenant, id string, data json.RawMessage) (ResourceRef, error) {
	ref := ResourceRef{Type: kind, TenantID: tenant, ID: id}
	if len(data) > 0 {
		var relation struct {
			AgentID   string `json:"agent_id"`
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(data, &relation); err != nil {
			return ref, err
		}
		ref.AgentID = relation.AgentID
		ref.SessionID = relation.SessionID
	}
	if kind == "agents" {
		ref.AgentID = id
	}
	if kind == "sessions" {
		ref.SessionID = id
	}
	return ref, nil
}
