package agentbench

import (
	"context"
	"fmt"
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
		return fmt.Errorf("orders backend needs a trusted tenant and records")
	}
	seen := map[[2]string]bool{}
	for _, record := range b.Records {
		key := [2]string{record.TenantID, record.OrderID}
		if record.TenantID == "" || record.OrderID == "" || record.Text == "" || seen[key] {
			return fmt.Errorf("orders backend has an incomplete or duplicate record")
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
		return "", false, fmt.Errorf("lookup requires only a nonempty string order_id")
	}
	for _, record := range b.Records {
		if record.TenantID == b.TenantID && record.OrderID == id {
			return record.Text, false, nil
		}
	}
	// Do not disclose whether the order exists in another tenant.
	return "NOT_FOUND", true, nil
}
