package management_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/management/adapters"
	mt "github.com/kart-io/goagent/management/internal/managementtest"
	"github.com/kart-io/goagent/revision"
)

func revisionDefinition(version string) (revision.Definition, map[string][]byte) {
	return revision.Definition{
			Config: []byte(fmt.Sprintf(`{"precision":9007199254740993,"mode":%q}`, version)), Prompts: map[string]string{"system": "prompt-" + version},
			Tools:  []byte(fmt.Sprintf(`[{"name":%q,"schema":{"type":"object"}}]`, "tool-"+version)),
			Models: map[string]revision.Reference{"chat": {ID: "model-" + version, Version: version}},
			Policy: revision.Reference{ID: "policy-" + version, Version: version}, Skills: map[string]string{"demo": "skills/demo"},
			Runtime: revision.Runtime{Kind: "native", Version: version, Digest: fmt.Sprintf("%x", sha256.Sum256([]byte("program-"+version)))},
		}, map[string][]byte{
			"skills/demo/SKILL.md": []byte("---\nname: demo\ndescription: archived skill\n---\ninstructions-" + version),
			"skills/demo/ref.txt":  []byte("reference-" + version), "template.txt": []byte("template-" + version),
		}
}

type revisionRuntime struct {
	values map[string]string
	r      mg.Resources
	wait   string
}

func (r *revisionRuntime) Inspect(_ context.Context, spec mg.RunSpec) (mg.RunEvidence, error) {
	if len(spec.Task.Waits) > 0 {
		return mg.RunEvidence{State: "waiting"}, nil
	}
	return mg.RunEvidence{State: "absent"}, nil
}

func (r *revisionRuntime) Run(ctx context.Context, spec mg.RunSpec) (mg.RunResult, error) {
	if string(spec.Task.Input) == `"wait"` && len(spec.ResumeInputs) == 0 {
		return mg.RunResult{Outcome: "waiting", Waits: []mg.WaitPoint{{ID: "question", Kind: r.wait, Key: "question", IntentHash: "original-intent", Info: []byte(`{"question":"continue?"}`)}}}, nil
	}
	// 此测试运行器用真实 Session 事件提供可 Fork 的完成边界。
	events, err := r.r.Sessions.Read(ctx, r.r.Session.LogID, 0)
	if err != nil {
		return mg.RunResult{}, err
	}
	end, err := session.NewEvent(constants.SessionEventTurnEnd, session.TurnEnd{})
	if err != nil {
		return mg.RunResult{}, err
	}
	var seq int64
	if len(events) > 0 {
		seq = events[len(events)-1].Seq
	}
	if _, err = r.r.Sessions.Append(ctx, r.r.Session.LogID, seq, end); err != nil {
		return mg.RunResult{}, err
	}
	out, err := json.Marshal(r.values)
	return mg.RunResult{Outcome: "completed", Output: out}, err
}

func revisionLoader(program, wait string) adapters.RevisionLoader {
	return adapters.RevisionLoader{
		Check: func(context.Context, revision.AgentRevision) (mg.Capabilities, error) {
			return mg.Capabilities{Approval: true, Input: true, Fork: true}, nil
		},
		Build: func(ctx context.Context, env adapters.RevisionEnvironment, resources mg.Resources) (mg.Binding, error) {
			skill, err := env.Skills.Load(ctx, "demo")
			if err != nil {
				return mg.Binding{}, err
			}
			ref, err := env.Skills.ReadReference(ctx, "demo", "ref.txt")
			if err != nil {
				return mg.Binding{}, err
			}
			template, err := env.Root.ReadFile("template.txt")
			if err != nil {
				return mg.Binding{}, err
			}
			var config struct {
				Precision json.Number `json:"precision"`
				Mode      string      `json:"mode"`
			}
			if err := json.Unmarshal(env.Revision.Definition.Config, &config); err != nil {
				return mg.Binding{}, err
			}
			var tools []struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(env.Revision.Definition.Tools, &tools); err != nil {
				return mg.Binding{}, err
			}
			values := map[string]string{"config": config.Mode, "precision": string(config.Precision), "tool": tools[0].Name, "model_id": env.Revision.Definition.Models["chat"].ID, "policy_id": env.Revision.Definition.Policy.ID, "program": program, "prompt": env.Revision.Definition.Prompts["system"], "instructions": skill.Body, "reference": string(ref), "template": string(template), "model": env.Revision.Definition.Models["chat"].Version, "policy": env.Revision.Definition.Policy.Version}
			return mg.Binding{Runtime: &revisionRuntime{values: values, r: resources, wait: wait}}, nil
		},
	}
}

func revisionWorker(t *testing.T, s *mg.Service) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- mg.NewWorker(s).Run(ctx) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("版本恢复工作器未退出")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func revisionReady(t *testing.T, f *mt.Fixture, id string) mg.Session {
	t.Helper()
	var se mg.Session
	require.Eventually(t, func() bool {
		var err error
		se, err = f.Service.GetSession(context.Background(), f.Actor, id)
		return err == nil && se.Status == "ready"
	}, 5*time.Second, 5*time.Millisecond)
	return se
}

func revisionResult(t *testing.T, f *mt.Fixture, id, want string) mg.Task {
	t.Helper()
	got := offlineExecutionWaitTask(t, f, id, mg.Succeeded)
	var values map[string]string
	require.NoError(t, json.Unmarshal(got.Result, &values))
	for key, value := range map[string]string{"config": want, "precision": "9007199254740993", "tool": "tool-" + want, "model_id": "model-" + want, "policy_id": "policy-" + want, "program": "program-" + want, "prompt": "prompt-" + want, "instructions": "instructions-" + want, "reference": "reference-" + want, "template": "template-" + want, "model": want, "policy": want} {
		require.Equal(t, value, values[key], key)
	}
	return got
}

func TestAgentRevisionSurvivesDeploymentAndBindsSessions(t *testing.T) {
	for _, wait := range []string{"input", "approval"} {
		t.Run(wait, func(t *testing.T) {
			ctx := context.Background()
			f := mt.NewFixture(t)
			path := t.TempDir()
			archive, err := revision.Open(path)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, archive.Close()) })
			d1, files := revisionDefinition("v1")
			r1, err := archive.Put(ctx, d1, files)
			require.NoError(t, err)
			factory, err := adapters.NewArchivedFactory(archive, map[revision.Runtime]adapters.RevisionLoader{d1.Runtime: revisionLoader("program-v1", wait)})
			require.NoError(t, err)
			cfg := f.Config()
			cfg.Factories = map[string]mg.Factory{"archived": factory}
			f.Service, err = mg.New(cfg)
			require.NoError(t, err)
			agent, err := f.Service.RegisterAgent(ctx, f.Actor, mg.RegisterAgentRequest{IdempotencyKey: "agent", Name: "归档版本"})
			require.NoError(t, err)
			v1, err := f.Service.SaveVersion(ctx, f.Actor, mg.SaveVersionRequest{IdempotencyKey: "save-v1", AgentID: agent.ID, Version: "v1", RunnerKind: "archived", ImplementationVersion: "v1", Config: d1.Config, RevisionID: r1.ID})
			require.NoError(t, err)
			agent, err = f.Service.PublishVersion(ctx, f.Actor, mg.PublishRequest{IdempotencyKey: "publish-v1", AgentID: agent.ID, Version: "v1", ExpectedRevision: agent.Revision})
			require.NoError(t, err)
			old, err := f.Service.CreateSession(ctx, f.Actor, mg.CreateSessionRequest{IdempotencyKey: "old", AgentID: agent.ID})
			require.NoError(t, err)
			require.Equal(t, r1.ID, old.RevisionID)
			stop := revisionWorker(t, f.Service)
			revisionReady(t, f, old.ID)
			task, err := f.Service.SubmitTask(ctx, f.Actor, mg.SubmitTaskRequest{IdempotencyKey: "waiting", SessionID: old.ID, Input: []byte(`"wait"`)})
			require.NoError(t, err)
			state := mg.WaitingInput
			if wait == "approval" {
				state = mg.WaitingApproval
			}
			waiting := offlineExecutionWaitTask(t, f, task.ID, state)
			stop()
			// 发布前输入可被覆盖，关闭并重开归档后旧运行仍只消费归档字节。
			d1.Prompts["system"] = "overwritten"
			for name := range files {
				files[name] = []byte("overwritten")
			}
			d2, files2 := revisionDefinition("v2")
			r2, err := archive.Put(ctx, d2, files2)
			require.NoError(t, err)
			require.NoError(t, archive.Close())
			reopened, err := revision.Open(path)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, reopened.Close()) })
			factory, err = adapters.NewArchivedFactory(reopened, map[revision.Runtime]adapters.RevisionLoader{d1.Runtime: revisionLoader("program-v1", wait), d2.Runtime: revisionLoader("program-v2", wait)})
			require.NoError(t, err)
			cfg.Factories = map[string]mg.Factory{"archived": factory}
			f.Service, err = mg.New(cfg)
			require.NoError(t, err)
			_, err = f.Service.SaveVersion(ctx, f.Actor, mg.SaveVersionRequest{IdempotencyKey: "overwrite-v1", AgentID: agent.ID, Version: "v1", RunnerKind: "archived", ImplementationVersion: "v2", Config: d2.Config, RevisionID: r2.ID})
			require.Error(t, err, "同名不可变版本不能被新归档覆盖")
			v2, err := f.Service.SaveVersion(ctx, f.Actor, mg.SaveVersionRequest{IdempotencyKey: "save-v2", AgentID: agent.ID, Version: "v2", RunnerKind: "archived", ImplementationVersion: "v2", Config: d2.Config, RevisionID: r2.ID})
			require.NoError(t, err)
			require.NotEqual(t, v1.ConfigHash, v2.ConfigHash)
			agent, err = f.Service.PublishVersion(ctx, f.Actor, mg.PublishRequest{IdempotencyKey: "publish-v2", AgentID: agent.ID, Version: "v2", ExpectedRevision: agent.Revision})
			require.NoError(t, err)
			fresh, err := f.Service.CreateSession(ctx, f.Actor, mg.CreateSessionRequest{IdempotencyKey: "new", AgentID: agent.ID})
			require.NoError(t, err)
			require.Equal(t, r2.ID, fresh.RevisionID)
			var resumeID string
			if wait == "approval" {
				approvals, err := f.Service.ListApprovals(ctx, f.Actor, mg.Query{TaskID: task.ID})
				require.NoError(t, err)
				require.Len(t, approvals.Items, 1)
				decided, err := f.Service.DecideApproval(ctx, f.Actor, mg.DecisionRequest{IdempotencyKey: "approve", ApprovalID: approvals.Items[0].ID, ExpectedRevision: approvals.Items[0].Revision, Decision: "approve"})
				require.NoError(t, err)
				resumeID = decided.CommandID
			} else {
				resumed, err := f.Service.ResumeTask(ctx, f.Actor, mg.ResumeTaskRequest{IdempotencyKey: "resume", TaskID: task.ID, ExpectedRevision: waiting.Revision, WaitID: waiting.Waits[0].ID, Input: []byte(`"yes"`)})
				require.NoError(t, err)
				resumeID = resumed.ID
			}

			// 模拟新宿主漏装旧程序：命令阻塞，原等待和已接纳回复不能被消费。
			unavailable, err := adapters.NewArchivedFactory(reopened, map[revision.Runtime]adapters.RevisionLoader{d2.Runtime: revisionLoader("program-v2", wait)})
			require.NoError(t, err)
			cfg.Factories = map[string]mg.Factory{"archived": unavailable}
			f.Service, err = mg.New(cfg)
			require.NoError(t, err)
			stopMissing := revisionWorker(t, f.Service)
			require.Eventually(t, func() bool {
				cmd, e := f.Service.GetCommand(ctx, f.Actor, resumeID)
				return e == nil && cmd.State == "blocked"
			}, 5*time.Second, 5*time.Millisecond)
			retained, err := f.Service.GetTask(ctx, f.Actor, task.ID)
			require.NoError(t, err)
			require.Equal(t, state, retained.State)
			require.Equal(t, waiting.Waits, retained.Waits)
			require.Equal(t, waiting.ExecutionKey, retained.ExecutionKey)
			require.Equal(t, r1.ID, retained.RevisionID)
			stopMissing()
			cfg.Factories = map[string]mg.Factory{"archived": factory}
			f.Service, err = mg.New(cfg)
			require.NoError(t, err)
			revisionWorker(t, f.Service)
			got := revisionResult(t, f, task.ID, "v1")
			require.Equal(t, r1.ID, got.RevisionID)
			revisionReady(t, f, fresh.ID)
			for _, tc := range []struct {
				se   mg.Session
				want string
			}{{old, "v1"}, {fresh, "v2"}} {
				next, err := f.Service.SubmitTask(ctx, f.Actor, mg.SubmitTaskRequest{IdempotencyKey: "next-" + tc.want, SessionID: tc.se.ID, Input: []byte(`"run"`)})
				require.NoError(t, err)
				revisionResult(t, f, next.ID, tc.want)
			}
			old, err = f.Service.GetSession(ctx, f.Actor, old.ID)
			require.NoError(t, err)
			child, err := f.Service.ForkSession(ctx, f.Actor, mg.ForkSessionRequest{IdempotencyKey: "fork", SessionID: old.ID, ExpectedRevision: old.Revision})
			require.NoError(t, err)
			require.Equal(t, r1.ID, child.RevisionID)
			revisionReady(t, f, child.ID)
			forkTask, err := f.Service.SubmitTask(ctx, f.Actor, mg.SubmitTaskRequest{IdempotencyKey: "fork-task", SessionID: child.ID, Input: []byte(`"run"`)})
			require.NoError(t, err)
			revisionResult(t, f, forkTask.ID, "v1")
			run, err := f.Service.GetRun(ctx, f.Actor, task.ID)
			require.NoError(t, err)
			require.Equal(t, r1.ID, run.Task.RevisionID)
			_, err = f.Service.GetSession(ctx, mg.Actor{TenantID: "other", Subject: f.Actor.Subject}, old.ID)
			require.ErrorIs(t, err, agentErrors.ErrManagementNotFound)
		})
	}
}

// 此工厂故意不自行验真，用于确认服务层不只信任宿主返回的能力声明。
type revisionGateFactory struct {
	*mt.Factory
	snapshot revision.AgentRevision
}

func (f *revisionGateFactory) ValidateRevision(context.Context, mg.Version) (revision.AgentRevision, mg.Capabilities, error) {
	return f.snapshot, f.Capabilities, nil
}

func TestAgentRevisionSaveAndPublishGates(t *testing.T) {
	ctx := context.Background()
	definition, files := revisionDefinition("v1")
	snapshot, err := revision.Seal(definition, files)
	require.NoError(t, err)
	for _, tc := range []string{"plain_factory", "invalid_id", "wrong_id", "tampered", "wrong_config", "wrong_program"} {
		t.Run(tc, func(t *testing.T) {
			f := mt.NewFixture(t)
			copy, err := revision.Seal(definition, files)
			require.NoError(t, err)
			factory := &revisionGateFactory{Factory: f.Factory, snapshot: copy}
			cfg := f.Config()
			cfg.Factories = map[string]mg.Factory{"archived": factory}
			if tc == "plain_factory" {
				cfg.Factories["archived"] = f.Factory
			}
			f.Service, err = mg.New(cfg)
			require.NoError(t, err)
			a, err := f.Service.RegisterAgent(ctx, f.Actor, mg.RegisterAgentRequest{IdempotencyKey: "agent", Name: "版本门禁"})
			require.NoError(t, err)
			r := mg.SaveVersionRequest{IdempotencyKey: "save", AgentID: a.ID, Version: "v1", RunnerKind: "archived", ImplementationVersion: "v1", Config: definition.Config, RevisionID: snapshot.ID}
			switch tc {
			case "invalid_id":
				r.RevisionID = strings.ToUpper(snapshot.ID)
			case "wrong_id":
				r.RevisionID = strings.Repeat("0", 64)
			case "tampered":
				factory.snapshot.Definition.Prompts["system"] = "被覆盖"
			case "wrong_config":
				r.Config = []byte(`{"precision":9007199254740992}`)
			case "wrong_program":
				r.ImplementationVersion = "v2"
			}
			_, err = f.Service.SaveVersion(ctx, f.Actor, r)
			require.Error(t, err)
			versions, err := f.Service.ListVersions(ctx, f.Actor, mg.Query{AgentID: a.ID})
			require.NoError(t, err)
			require.Empty(t, versions.Items)
		})
	}

	f := mt.NewFixture(t)
	archive, err := revision.Open(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, archive.Close()) })
	one, err := archive.Put(ctx, definition, files)
	require.NoError(t, err)
	definition.Prompts["system"] = "同程序新提示词"
	two, err := archive.Put(ctx, definition, files)
	require.NoError(t, err)
	factory, err := adapters.NewArchivedFactory(archive, map[revision.Runtime]adapters.RevisionLoader{definition.Runtime: revisionLoader("program-v1", "input")})
	require.NoError(t, err)
	cfg := f.Config()
	cfg.Factories = map[string]mg.Factory{"archived": factory}
	f.Service, err = mg.New(cfg)
	require.NoError(t, err)
	a, err := f.Service.RegisterAgent(ctx, f.Actor, mg.RegisterAgentRequest{IdempotencyKey: "agent", Name: "发布门禁"})
	require.NoError(t, err)
	r := mg.SaveVersionRequest{IdempotencyKey: "save-v1", AgentID: a.ID, Version: "v1", RunnerKind: "archived", ImplementationVersion: "v1", Config: definition.Config, RevisionID: one.ID}
	v1, err := f.Service.SaveVersion(ctx, f.Actor, r)
	require.NoError(t, err)
	retry, err := f.Service.SaveVersion(ctx, f.Actor, r)
	require.NoError(t, err)
	require.Equal(t, v1, retry)
	r.RevisionID = two.ID
	_, err = f.Service.SaveVersion(ctx, f.Actor, r)
	require.ErrorIs(t, err, agentErrors.ErrManagementConflict)
	r.Version, r.IdempotencyKey = "v2", "save-v2"
	v2, err := f.Service.SaveVersion(ctx, f.Actor, r)
	require.NoError(t, err)
	require.NotEqual(t, v1.ConfigHash, v2.ConfigHash, "提示词变化必须改变恢复身份")
	a, err = f.Service.PublishVersion(ctx, f.Actor, mg.PublishRequest{IdempotencyKey: "publish-v1", AgentID: a.ID, Version: "v1", ExpectedRevision: a.Revision})
	require.NoError(t, err)
	require.NoError(t, archive.Close())
	_, err = f.Service.PublishVersion(ctx, f.Actor, mg.PublishRequest{IdempotencyKey: "publish-v2", AgentID: a.ID, Version: "v2", ExpectedRevision: a.Revision})
	require.Error(t, err)
	got, err := f.Service.GetAgent(ctx, f.Actor, a.ID)
	require.NoError(t, err)
	require.Equal(t, a, got, "依赖不可用不能修改已发布版本")
}
