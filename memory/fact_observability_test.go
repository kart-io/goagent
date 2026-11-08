package memory

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type factObserverFixture struct{ events []MemoryEvent }

func (o *factObserverFixture) ObserveMemory(event MemoryEvent) { o.events = append(o.events, event) }

type panickingFactObserver struct{}

func (panickingFactObserver) ObserveMemory(MemoryEvent) { panic("observer failure") }

func TestFactServiceEmitsBodyFreeOperationEvents(t *testing.T) {
	store := newFactStoreFixture()
	scope := MemoryScope{TenantID: "tenant-observe", OwnerKind: OwnerAgent, OwnerID: "user", AgentID: "agent-observe"}
	access := MemoryAccess{Principal: Principal{TenantID: scope.TenantID, UserID: "user", AgentID: scope.AgentID}, ReadScopes: []MemoryScope{scope}, WriteScopes: []MemoryScope{scope}}
	observer := &factObserverFixture{}
	limits := DefaultFactLimits()
	limits.MaxFactBytes, limits.MaxMetadataBytes, limits.MaxPageSize, limits.WorkerConcurrency, limits.MaxPendingPerTenant = 1024, 1024, 200, 4, 100
	service, err := NewDefaultMemoryService(MemoryServiceConfig{Store: store, Capabilities: store.capabilities(), Limits: limits, Observer: observer})
	if err != nil {
		t.Fatal(err)
	}
	service.index = &fixtureIndex{}
	service.embedders = fixtureEmbedderResolver{}
	receipt, err := service.Save(context.Background(), WriteScope{Access: access, Target: scope}, "observe-save", FactInput{Content: "private observability body", RecallClass: RecallAlways})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Get(context.Background(), access, receipt.FactID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.List(context.Background(), access, FactFilter{}, PageRequest{Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Recall(context.Background(), access, "", RecallOptions{}); err != nil {
		t.Fatal(err)
	}
	denied := access
	denied.ReadScopes = nil
	if _, err = service.Get(context.Background(), denied, receipt.FactID); err == nil {
		t.Fatal("expected authorization failure")
	}
	if len(observer.events) < 4 {
		t.Fatalf("operation event count=%d want at least 4: %+v", len(observer.events), observer.events)
	}
	encoded := fmt.Sprintf("%+v", observer.events)
	if strings.Contains(encoded, "private observability body") {
		t.Fatalf("observer event leaked fact content: %s", encoded)
	}
	seen := map[string]bool{}
	for _, event := range observer.events {
		seen[event.Operation] = true
		if event.Operation == "" || event.Duration < 0 {
			t.Fatalf("invalid event: %+v", event)
		}
	}
	for _, operation := range []string{"save", "get", "list", "recall"} {
		if !seen[operation] {
			t.Fatalf("missing %s event: %+v", operation, observer.events)
		}
	}
	last := observer.events[len(observer.events)-1]
	if last.Operation != "get" || last.ErrorCode != "authorization" {
		t.Fatalf("authorization failure was not classified safely: %+v", last)
	}
}

func TestFactObserverPanicDoesNotChangeCommittedOperations(t *testing.T) {
	store := newFactStoreFixture()
	limits := DefaultFactLimits()
	limits.MaxFactBytes, limits.MaxMetadataBytes, limits.MaxPageSize, limits.WorkerConcurrency, limits.MaxPendingPerTenant = 1024, 1024, 200, 4, 100
	service, err := NewDefaultMemoryService(MemoryServiceConfig{Store: store, Capabilities: store.capabilities(), Limits: limits, Observer: panickingFactObserver{}})
	if err != nil {
		t.Fatal(err)
	}
	scope := MemoryScope{TenantID: "tenant-observer-panic", OwnerKind: OwnerUser, OwnerID: "user"}
	access := MemoryAccess{Principal: Principal{TenantID: scope.TenantID, UserID: scope.OwnerID, AgentID: "agent"}, ReadScopes: []MemoryScope{scope}, WriteScopes: []MemoryScope{scope}}
	receipt, err := service.Save(context.Background(), WriteScope{Access: access, Target: scope}, "observer-panic-save", FactInput{Content: "committed"})
	if err != nil || receipt.FactID == "" {
		t.Fatalf("observer panic changed committed Save result: receipt=%+v err=%v", receipt, err)
	}
	if _, err = service.Get(context.Background(), access, receipt.FactID); err != nil {
		t.Fatalf("observer panic interrupted subsequent operations: %v", err)
	}
}

func TestFactWorkerObserverPanicDoesNotStopWorker(t *testing.T) {
	svc, store, scope := testMemoryService(t)
	if _, err := svc.Save(context.Background(), scope, "worker-observer-panic", FactInput{Content: "index me"}); err != nil {
		t.Fatal(err)
	}
	index := &fixtureIndex{}
	worker, err := NewFactWorker(FactWorkerConfig{Store: store, Index: index, Embedders: fixtureEmbedderResolver{}, BatchSize: 10, Concurrency: 2, Observer: panickingFactObserver{}})
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Run(context.Background()); err != nil {
		t.Fatalf("observer panic stopped worker: %v", err)
	}
	if len(index.applied) != 1 {
		t.Fatalf("worker failed to apply mutation after observer panic: %+v", index.applied)
	}
}
