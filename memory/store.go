package memory

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/store"
)

// RecordStore 保存记录正文，是长期记忆的权威来源；向量索引可由它重建。
type RecordStore interface {
	Put(ctx context.Context, r Record) error
	// Get 在记录不存在时返回 CodeStoreNotFound。
	Get(ctx context.Context, namespace []string, id string) (Record, error)
	List(ctx context.Context, namespace []string) ([]Record, error)
	Delete(ctx context.Context, namespace []string, id string) error
	Clear(ctx context.Context, namespace []string) error
}

// NewRecordStore 把记录以 JSON 文本存入 st 的 {"memory","records",命名空间...} 下，适用于任意 store.Store 后端。
func NewRecordStore(st store.Store) RecordStore { return &recordStore{st: st} }

type recordStore struct{ st store.Store }

func recordsNS(ns []string) []string { return append([]string{"memory", "records"}, ns...) }

func (s *recordStore) Put(ctx context.Context, r Record) error {
	if err := r.validate(); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return s.st.Put(ctx, recordsNS(r.Namespace), r.ID, string(b))
}

func (s *recordStore) Get(ctx context.Context, ns []string, id string) (Record, error) {
	v, err := s.st.Get(ctx, recordsNS(ns), id)
	if err != nil {
		return Record{}, err
	}
	return decodeRecord(v)
}

func (s *recordStore) List(ctx context.Context, ns []string) ([]Record, error) {
	values, err := s.st.Search(ctx, recordsNS(ns), nil)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(values))
	for _, v := range values {
		r, err := decodeRecord(v)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Record) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

func (s *recordStore) Delete(ctx context.Context, ns []string, id string) error {
	err := s.st.Delete(ctx, recordsNS(ns), id)
	if isNotFound(err) {
		return nil
	}
	return err
}

func (s *recordStore) Clear(ctx context.Context, ns []string) error {
	return s.st.Clear(ctx, recordsNS(ns))
}

func decodeRecord(v *store.Value) (Record, error) {
	text, ok := v.Value.(string)
	if !ok {
		return Record{}, agentErrors.Newf(agentErrors.CodeStoreSerialization, "记忆记录不是 JSON 文本").WithComponent("memory").WithOperation("decode")
	}
	var r Record
	err := json.Unmarshal([]byte(text), &r)
	return r, err
}

func isNotFound(err error) bool { return agentErrors.IsCode(err, agentErrors.CodeStoreNotFound) }

// JobStatus 是抽取任务的状态。
type JobStatus string

const (
	JobPending JobStatus = "pending"
	JobDone    JobStatus = "done"
	JobFailed  JobStatus = "failed"
)

// Job 是一轮结束后登记的抽取任务。Key 为"会话/turn/start 序号"，重复登记不产生新任务。
// Aborted 的轮次只提炼教训（reflection），其余轮次抽取事实与情景。
type Job struct {
	Key       string    `json:"key"`
	Namespace []string  `json:"namespace"`
	Session   string    `json:"session"`
	From      int64     `json:"from"`
	To        int64     `json:"to"`
	Aborted   bool      `json:"aborted,omitzero"`
	Status    JobStatus `json:"status"`
	Attempts  int       `json:"attempts,omitzero"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

var jobsNS = []string{"memory", "jobs"}

func putJob(ctx context.Context, st store.Store, j Job) error {
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return st.Put(ctx, jobsNS, j.Key, string(b))
}

func getJob(ctx context.Context, st store.Store, key string) (Job, bool, error) {
	v, err := st.Get(ctx, jobsNS, key)
	if isNotFound(err) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, err
	}
	text, _ := v.Value.(string)
	var j Job
	err = json.Unmarshal([]byte(text), &j)
	return j, err == nil, err
}

// pendingJobs 按登记时间返回待处理的任务。
func pendingJobs(ctx context.Context, st store.Store) ([]Job, error) {
	values, err := st.Search(ctx, jobsNS, nil)
	if err != nil {
		return nil, err
	}
	var out []Job
	for _, v := range values {
		text, _ := v.Value.(string)
		var j Job
		if err := json.Unmarshal([]byte(text), &j); err != nil {
			return nil, err
		}
		if j.Status == JobPending {
			out = append(out, j)
		}
	}
	slices.SortFunc(out, func(a, b Job) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.Key, b.Key)
	})
	return out, nil
}

func (j Job) source() Source { return Source{Session: j.Session, From: j.From, To: j.To, Job: j.Key} }
