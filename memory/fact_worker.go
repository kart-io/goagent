package memory

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"
)

// FactWorkerConfig 配置索引 outbox Worker。
type FactWorkerConfig struct {
	Store       FactStore
	Index       MemoryIndex
	Embedders   EmbedderResolver
	BatchSize   int
	Concurrency int
	Observer    MemoryObserver
}

// FactWorker 将持久 outbox 中的事实变更应用到可重建索引。
type FactWorker struct{ config FactWorkerConfig }

var _ MemoryMaintenance = (*FactWorker)(nil)

// NewFactWorker 创建索引和维护 Worker。
func NewFactWorker(config FactWorkerConfig) (*FactWorker, error) {
	if config.Store == nil || config.Index == nil || config.Embedders == nil || config.BatchSize < 1 || config.Concurrency < 1 {
		return nil, &MemoryError{Kind: ErrProviderCapability, Op: "创建事实记忆 Worker"}
	}
	return &FactWorker{config: config}, nil
}

// Run 处理当前所有可领取的索引 mutation；宿主可周期性调用并在取消时停止。
func (w *FactWorker) Run(ctx context.Context) error {
	var failures []error
	for {
		if err := ctx.Err(); err != nil {
			return errors.Join(errors.Join(failures...), err)
		}
		batch, err := w.config.Store.ClaimIndexMutations(ctx, w.config.BatchSize)
		if err != nil {
			return errors.Join(errors.Join(failures...), err)
		}
		if len(batch) == 0 {
			clears, clearErr := w.config.Store.ClaimClearOperations(ctx, w.config.BatchSize)
			if clearErr != nil {
				return errors.Join(errors.Join(failures...), clearErr)
			}
			for _, operation := range clears {
				started := time.Now()
				err := w.config.Store.AckClearOperation(ctx, operation.ID, ClearProgress{Done: true})
				count := 0
				if err == nil {
					count = 1
				}
				observeMemory(w.config.Observer, MemoryEvent{Operation: "scope_clear", Duration: time.Since(started), Count: count, ErrorCode: memoryErrorCode(err)})
				if err != nil {
					failures = append(failures, err)
				}
			}
			if err := w.runRebuilds(ctx); err != nil {
				failures = append(failures, err)
			}
			return errors.Join(failures...)
		}
		sem := make(chan struct{}, w.config.Concurrency)
		var wg sync.WaitGroup
		var mu sync.Mutex
		var dispatchErr error
		for _, mutation := range batch {
			mutation := mutation
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				dispatchErr = ctx.Err()
			}
			if dispatchErr != nil {
				break
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				if err := w.apply(ctx, mutation); err != nil {
					mu.Lock()
					failures = append(failures, err)
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		if dispatchErr != nil {
			return errors.Join(errors.Join(failures...), dispatchErr)
		}
	}
}

func (w *FactWorker) runRebuilds(ctx context.Context) (retErr error) {
	rebuilds, err := w.config.Store.ClaimRebuilds(ctx, 1)
	if err != nil {
		return err
	}
	for _, rebuild := range rebuilds {
		started := time.Now()
		processed := 0
		defer func() {
			observeMemory(w.config.Observer, MemoryEvent{Operation: "index_rebuild", Duration: time.Since(started), Count: processed, ErrorCode: memoryErrorCode(retErr)})
		}()
		page, err := w.config.Store.ScanRebuildFacts(ctx, rebuild.ID, rebuild.Cursor, w.config.BatchSize)
		if err != nil {
			return err
		}
		for _, fact := range page.Items {
			embedder, err := w.config.Embedders.Resolve(ctx, rebuild.Profile.ID)
			if err != nil {
				return err
			}
			profile := embedder.Profile()
			if profile.ID != rebuild.Profile.ID {
				return &MemoryError{Kind: ErrProviderCapability, Op: "重建 Embedder Profile 不匹配"}
			}
			vector, err := embedder.Embed(ctx, fact.Content)
			if err != nil {
				return err
			}
			if profile.Dimension > 0 && len(vector) != profile.Dimension {
				return &MemoryError{Kind: ErrProviderCapability, Op: "重建嵌入维度不匹配"}
			}
			mutation := IndexMutation{ID: MutationID(string(rebuild.ID) + ":" + string(fact.ID) + ":" + strconv.FormatUint(uint64(fact.MutationVersion), 10)), FactID: fact.ID, Scope: fact.Scope, Revision: fact.Revision, MutationVersion: fact.MutationVersion, StoreSequence: fact.StoreSequence, ProfileID: rebuild.Profile.ID, Generation: rebuild.Generation, Action: MutationUpsert, Vector: vector}
			if err = w.config.Index.ApplyMutation(ctx, mutation); err != nil {
				return &MemoryError{Kind: ErrProviderCapability, Op: "应用重建索引页", Cause: err}
			}
			processed++
		}
		if err = w.config.Store.AdvanceRebuild(ctx, rebuild.ID, RebuildProgress{LeaseToken: rebuild.LeaseToken, Cursor: page.NextCursor, CaughtUp: page.CaughtUp, Done: page.Done}); err != nil {
			return err
		}
		if page.Done {
			active, err := w.config.Store.ActiveIndexProfile(ctx)
			if err != nil {
				return err
			}
			if err = w.config.Store.SwitchActiveGeneration(ctx, active.Generation, rebuild.ID, page.CaughtUp, rebuild.LeaseToken); err != nil && !errors.Is(err, ErrRevisionConflict) {
				return err
			}
		}
	}
	return nil
}

func (w *FactWorker) apply(ctx context.Context, mutation IndexMutation) (retErr error) {
	started := time.Now()
	defer func() {
		count := 0
		if retErr == nil {
			count = 1
		}
		observeMemory(w.config.Observer, MemoryEvent{Operation: "index_mutation", Duration: time.Since(started), Count: count, ErrorCode: memoryErrorCode(retErr)})
	}()
	result := MutationApplied
	if mutation.Action == MutationUpsert {
		input, err := w.config.Store.LoadIndexInput(ctx, mutation)
		if err != nil {
			return w.fail(ctx, mutation, err)
		}
		if !input.Active || input.MutationVersion != mutation.MutationVersion {
			return w.config.Store.AckIndexMutation(ctx, mutation, MutationObsolete)
		}
		state, err := w.config.Store.ActiveIndexProfile(ctx)
		if err != nil {
			return w.fail(ctx, mutation, err)
		}
		profileID, generation := state.Profile.ID, state.Generation
		if mutation.ProfileID != "" {
			profileID = mutation.ProfileID
			generation = mutation.Generation
		}
		embedder, err := w.config.Embedders.Resolve(ctx, profileID)
		if err != nil {
			return w.fail(ctx, mutation, err)
		}
		profile := embedder.Profile()
		if profile.ID != profileID {
			return w.fail(ctx, mutation, &MemoryError{Kind: ErrProviderCapability, Op: "Embedder Profile 不匹配"})
		}
		vector, err := embedder.Embed(ctx, input.Content)
		if err != nil {
			return w.fail(ctx, mutation, err)
		}
		if profile.Dimension > 0 && len(vector) != profile.Dimension {
			return w.fail(ctx, mutation, &MemoryError{Kind: ErrProviderCapability, Op: "嵌入维度不匹配"})
		}
		mutation.Vector = vector
		mutation.ProfileID = profileID
		mutation.Generation = generation
	}
	if err := w.config.Index.ApplyMutation(ctx, mutation); err != nil {
		return w.fail(ctx, mutation, err)
	}
	if err := w.config.Store.AckIndexMutation(ctx, mutation, result); err != nil {
		return &MemoryError{Kind: ErrProviderCapability, Op: "确认索引 mutation", Cause: err}
	}
	return nil
}

func (w *FactWorker) fail(ctx context.Context, mutation IndexMutation, cause error) error {
	ackErr := w.config.Store.AckIndexMutation(ctx, mutation, MutationFailed)
	return &MemoryError{Kind: ErrProviderCapability, Op: "应用事实索引 mutation", Cause: errors.Join(cause, ackErr)}
}

// Status 查询维护状态。
func (w *FactWorker) Status(ctx context.Context, access MaintenanceAccess, filter MaintenanceFilter, page PageRequest) (MaintenancePage, error) {
	return w.config.Store.ListMaintenance(ctx, access, filter, page)
}

// Retry 请求 Store 重试一条失败 mutation。
func (w *FactWorker) Retry(ctx context.Context, access MaintenanceAccess, id MutationID) error {
	return w.config.Store.RetryMutation(ctx, access, id)
}

// ClearStatus 查询作用域清空任务。
func (w *FactWorker) ClearStatus(ctx context.Context, access MaintenanceAccess, id ClearOperationID) (ClearOperation, error) {
	return w.config.Store.GetClearOperation(ctx, MemoryAccess{Principal: Principal{TenantID: access.TenantID}, ReadScopes: access.AuthorizedScopes}, id)
}

// StartRebuild 在全局维护访问校验后创建新索引代际。
func (w *FactWorker) StartRebuild(ctx context.Context, access MaintenanceAccess, profile EmbeddingProfile) (RebuildID, error) {
	if !hasCapability(access.Capabilities, "memory.rebuild") || access.TenantID == "" || access.OperatorID == "" {
		return "", &MemoryError{Kind: ErrAuthorization}
	}
	rebuild, err := w.config.Store.CreateRebuild(ctx, MaintenancePrincipal{TenantID: access.TenantID, OperatorID: access.OperatorID}, profile)
	return rebuild.ID, err
}

// RebuildStatus 查询索引重建状态。
func (w *FactWorker) RebuildStatus(ctx context.Context, access MaintenanceAccess, id RebuildID) (Rebuild, error) {
	return w.config.Store.GetRebuild(ctx, access, id)
}

func hasCapability(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
