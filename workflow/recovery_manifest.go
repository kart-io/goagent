package workflow

import (
	"context"
	"maps"

	agentErrors "github.com/kart-io/goagent/errors"
)

// RecoveryManifest is a recovery identity manifest that the host can archive;
// it contains no business state or resource references.
type RecoveryManifest struct {
	Version      string            `json:"version"`
	Plan         string            `json:"plan"`
	StateVersion string            `json:"state_version,omitempty"`
	Components   map[string]string `json:"components,omitempty"`
}

// RunCompatibility only states whether the declared recovery identity matches;
// it is neither a recovery authorization nor a proof of idempotency.
type RunCompatibility struct {
	RunID      string   `json:"run_id"`
	Status     string   `json:"status"`
	Compatible bool     `json:"compatible"`
	Blockers   []string `json:"blockers,omitempty"`
}

// CompatibilityReport is the per-run read-only check result; multiple reads do
// not form a consistent snapshot.
type CompatibilityReport struct {
	Manifest RecoveryManifest   `json:"manifest"`
	Runs     []RunCompatibility `json:"runs"`
}

// Manifest returns an independent metadata copy of the current compiled plan.
func (d *DurableGraph[T]) Manifest() RecoveryManifest {
	if d == nil {
		return RecoveryManifest{}
	}
	return RecoveryManifest{Version: d.version, Plan: d.plan, StateVersion: d.stateVersion, Components: maps.Clone(d.components)}
}

// CheckCompatibility reuses Diagnose to read known runs; it does not execute
// nodes, decode business data, approve, or write.
func (d *DurableGraph[T]) CheckCompatibility(ctx context.Context, ids ...string) (*CompatibilityReport, error) {
	if d == nil || d.store == nil || d.graph == nil || ctx == nil || len(ids) == 0 {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "兼容检查需要已编译图、上下文和运行ID").WithComponent("workflow").WithOperation("check_compatibility")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "运行ID不能为空或重复").WithComponent("workflow").WithOperation("check_compatibility")
		}
		seen[id] = true
	}
	out := &CompatibilityReport{Manifest: d.Manifest()}
	for _, id := range ids {
		report, err := d.Diagnose(ctx, id)
		if err != nil {
			return nil, err
		}
		out.Runs = append(out.Runs, RunCompatibility{RunID: id, Status: report.Status, Compatible: report.Status != "blocked" && report.Status != "not_found", Blockers: append([]string(nil), report.Blockers...)})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
