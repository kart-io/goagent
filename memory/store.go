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
