package workflow

import (
	"context"
	"maps"

	agentErrors "github.com/kart-io/goagent/errors"
)

// RecoveryManifest 是宿主可归档的恢复身份清单，不包含业务状态或资源引用。
type RecoveryManifest struct {
	Version      string            `json:"version"`
	Plan         string            `json:"plan"`
	StateVersion string            `json:"state_version,omitempty"`
	Components   map[string]string `json:"components,omitempty"`
}

// RunCompatibility 仅说明声明的恢复身份是否匹配，不构成恢复授权或幂等证明。
type RunCompatibility struct {
	RunID      string   `json:"run_id"`
	Status     string   `json:"status"`
	Compatible bool     `json:"compatible"`
	Blockers   []string `json:"blockers,omitempty"`
}

// CompatibilityReport 是逐运行只读核对结果；多次读取不是一致性快照。
type CompatibilityReport struct {
	Manifest RecoveryManifest   `json:"manifest"`
	Runs     []RunCompatibility `json:"runs"`
}

// Manifest 返回当前编译计划的独立元数据副本。
func (d *DurableGraph[T]) Manifest() RecoveryManifest {
	if d == nil {
		return RecoveryManifest{}
	}
	return RecoveryManifest{Version: d.version, Plan: d.plan, StateVersion: d.stateVersion, Components: maps.Clone(d.components)}
}

// CheckCompatibility 复用Diagnose读取已知运行，不执行节点、业务解码、审批或写入。
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
