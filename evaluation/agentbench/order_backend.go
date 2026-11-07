package agentbench

import (
	"context"

	agentErrors "github.com/kart-io/goagent/errors"
)

// OrderLookupBackend is an isolated test database with a host-bound tenant.
// It never receives answer/argument labels and never accepts a model tenant ID.
type OrderLookupBackend struct {
	TenantID string              `json:"tenant_id"`
	Records  []OrderLookupRecord `json:"records"`
}

type OrderLookupRecord struct {
	TenantID string `json:"tenant_id"`
	OrderID  string `json:"order_id"`
	Text     string `json:"text"`
}

func (b *OrderLookupBackend) validate() error {
	if b.TenantID == "" || len(b.Records) == 0 {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "orders backend needs a trusted tenant and records").WithComponent("evaluation/agentbench").WithOperation("validate")
	}
	seen := map[[2]string]bool{}
	for _, record := range b.Records {
		key := [2]string{record.TenantID, record.OrderID}
		if record.TenantID == "" || record.OrderID == "" || record.Text == "" || seen[key] {
			return agentErrors.Newf(agentErrors.CodeInvalidConfig, "orders backend has an incomplete or duplicate record").WithComponent("evaluation/agentbench").WithOperation("validate")
		}
		seen[key] = true
	}
	return nil
}

func (b *OrderLookupBackend) query(ctx context.Context, args map[string]any) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	id, ok := args["order_id"].(string)
	if len(args) != 1 || !ok || id == "" {
		return "", false, agentErrors.Newf(agentErrors.CodeInvalidInput, "lookup requires only a nonempty string order_id").WithComponent("evaluation/agentbench").WithOperation("query")
	}
	for _, record := range b.Records {
		if record.TenantID == b.TenantID && record.OrderID == id {
			return record.Text, false, nil
		}
	}
	// Do not disclose whether the order exists in another tenant.
	return "NOT_FOUND", true, nil
}
