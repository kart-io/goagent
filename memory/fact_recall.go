package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"
)

// recall 实现候选检索后的权威 hydrate、策略过滤与预算截断。
func (s *DefaultMemoryService) recall(ctx context.Context, access MemoryAccess, query string, options RecallOptions) (RecallResult, error) {
	if err := validateReadAccess(access); err != nil {
		return RecallResult{}, err
	}
	limit := options.Limit
	if limit == 0 {
		limit = s.limits.DefaultPageSize
	}
	if limit < 1 || limit > s.limits.MaxPageSize {
		return RecallResult{}, &MemoryError{Kind: ErrCapacityExceeded, Op: "事实召回条数超限"}
	}
	if options.MinScore < 0 || options.MinScore > 1 {
		return RecallResult{}, &MemoryError{Kind: ErrInvalidFact, Op: "召回阈值必须位于 [0,1]"}
	}
	maxBytes := options.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 8 * 1024
	}
	maxTokens := options.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	state, err := s.store.ActiveIndexProfile(ctx)
	if err != nil {
		return RecallResult{}, err
	}
	result := RecallResult{Snapshot: SnapshotRef{ScopeFingerprint: scopeFingerprint(access), PolicyVersion: "fact-recall-v1", EmbeddingProfileID: state.Profile.ID, IndexGeneration: state.Generation}}
	selected := map[FactID]bool{}
	var candidates []FactMatch
	if strings.TrimSpace(query) != "" {
		embedder, err := s.embedders.Resolve(ctx, state.Profile.ID)
		if err != nil {
			return RecallResult{}, err
		}
		vector, err := embedder.Embed(ctx, query)
		if err != nil {
			return RecallResult{}, err
		}
		if state.Profile.Dimension > 0 && len(vector) != state.Profile.Dimension {
			return RecallResult{}, &MemoryError{Kind: ErrProviderCapability, Op: "查询嵌入维度不匹配"}
		}
		page, err := s.index.Search(ctx, IndexQuery{Scopes: append([]MemoryScope(nil), access.ReadScopes...), ProfileID: state.Profile.ID, Generation: state.Generation, Vector: vector, Limit: s.limits.MaxPageSize})
		if err != nil {
			return RecallResult{}, err
		}
		for _, candidate := range page.Items {
			if candidate.ProfileID != state.Profile.ID || candidate.Generation != state.Generation || candidate.Score < options.MinScore || candidate.Score < 0 || candidate.Score > 1 {
				continue
			}
			fact, err := s.store.Get(ctx, access, candidate.FactID, candidate.Revision)
			if err != nil {
				continue
			}
			if !scopeAllowed(access.ReadScopes, fact.Scope) || fact.Status != FactActive || (!fact.ExpiresAt.IsZero() && !s.now().Before(fact.ExpiresAt)) {
				continue
			}
			if !matchesFactFilter(fact, FactFilter{Tags: options.Tags}) {
				continue
			}
			candidates = append(candidates, FactMatch{Fact: fact, Score: candidate.Score, MatchKind: MatchSemantic})
		}
	}
	// 常驻事实独立于语义分数，但仍走同一授权、TTL 与预算约束。
	page, err := s.List(ctx, access, FactFilter{}, PageRequest{Limit: s.limits.MaxPageSize})
	if err != nil {
		return RecallResult{}, err
	}
	var always []Fact
	for _, fact := range page.Items {
		if fact.RecallClass == RecallAlways && matchesFactFilter(fact, FactFilter{Tags: options.Tags}) {
			always = append(always, fact)
		}
	}
	sort.Slice(always, func(i, j int) bool {
		if always[i].Priority != always[j].Priority {
			return always[i].Priority > always[j].Priority
		}
		return always[i].ID < always[j].ID
	})
	for _, fact := range always {
		if !selected[fact.ID] {
			candidates = append(candidates, FactMatch{Fact: fact, Score: 1, MatchKind: MatchAlways})
			selected[fact.ID] = true
		}
	}
	// 语义候选由 Index 的相关度排序保证；重复 ID 只保留首个最高分结果。
	var unique []FactMatch
	for _, match := range candidates {
		if selected[match.Fact.ID] && match.MatchKind != MatchAlways {
			continue
		}
		if match.MatchKind != MatchAlways {
			selected[match.Fact.ID] = true
		}
		unique = append(unique, match)
	}
	result.Matches = nil
	usedBytes, usedTokens := 0, 0
	for _, match := range unique {
		if len(result.Matches) >= limit {
			break
		}
		b := len([]byte(match.Fact.Content))
		tokens := (utf8.RuneCountInString(match.Fact.Content) + 3) / 4
		if usedBytes+b > maxBytes || usedTokens+tokens > maxTokens {
			continue
		}
		result.Matches = append(result.Matches, match)
		usedBytes += b
		usedTokens += tokens
		result.Snapshot.Facts = append(result.Snapshot.Facts, SnapshotFactRef{FactID: match.Fact.ID, Revision: match.Fact.Revision, MutationVersion: match.Fact.MutationVersion, Score: match.Score})
	}
	return result, nil
}

func (s *DefaultMemoryService) resolveSnapshot(ctx context.Context, access MemoryAccess, snapshot SnapshotRef) (RecallResult, error) {
	if err := validateReadAccess(access); err != nil {
		return RecallResult{}, err
	}
	if snapshot.ScopeFingerprint == "" || snapshot.ScopeFingerprint != scopeFingerprint(access) {
		return RecallResult{}, &MemoryError{Kind: ErrSnapshotInvalidated, Op: "事实快照授权范围已变化"}
	}
	state, err := s.store.ActiveIndexProfile(ctx)
	if err != nil {
		return RecallResult{}, err
	}
	if state.Profile.ID != snapshot.EmbeddingProfileID || state.Generation != snapshot.IndexGeneration {
		return RecallResult{}, &MemoryError{Kind: ErrSnapshotInvalidated, Op: "事实快照索引代际已变化"}
	}
	result := RecallResult{Snapshot: snapshot}
	for _, ref := range snapshot.Facts {
		fact, err := s.store.Get(ctx, access, ref.FactID, ref.Revision)
		if err != nil || fact.MutationVersion != ref.MutationVersion || fact.Status != FactActive || (!fact.ExpiresAt.IsZero() && !s.now().Before(fact.ExpiresAt)) {
			return RecallResult{}, &MemoryError{Kind: ErrSnapshotInvalidated, Op: "事实快照引用已失效", Cause: err}
		}
		result.Matches = append(result.Matches, FactMatch{Fact: fact, Score: ref.Score, MatchKind: MatchSemantic})
	}
	return result, nil
}

func matchesFactFilter(fact Fact, filter FactFilter) bool {
	if len(filter.Tags) == 0 {
		return true
	}
	raw, ok := fact.Metadata["tags"]
	if !ok {
		return false
	}
	var tags []string
	switch value := raw.(type) {
	case []string:
		tags = value
	case []any:
		for _, item := range value {
			if tag, ok := item.(string); ok {
				tags = append(tags, tag)
			}
		}
	}
	for _, want := range filter.Tags {
		found := false
		for _, tag := range tags {
			if tag == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func scopeFingerprint(access MemoryAccess) string {
	scopes := append([]MemoryScope(nil), access.ReadScopes...)
	sort.Slice(scopes, func(i, j int) bool {
		a, _ := json.Marshal(scopes[i])
		b, _ := json.Marshal(scopes[j])
		return string(a) < string(b)
	})
	data, _ := json.Marshal(struct {
		Principal Principal
		Scopes    []MemoryScope
	}{access.Principal, scopes})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
