package management

import (
	"github.com/kart-io/goagent/codec/json"
	"testing"
	"time"
)

func TestReviewRecoveredDuration(t *testing.T) {
	b := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	end := b.Add(10 * time.Second)
	r := Run{Task: Task{Meta: Meta{CreatedAt: b}, FinishedAt: &end}, Attempts: []Attempt{{Meta: Meta{CreatedAt: b}, Number: 1}, {Meta: Meta{CreatedAt: b.Add(8 * time.Second)}, Number: 2, FinishedAt: &end}}}
	projectDurations(&r, end)
	if r.ExecutionMillis != 2000 {
		t.Fatalf("仅新Attempt可确定执行2秒，不能把未结束旧Attempt重复累计：execution=%d elapsed=%d", r.ExecutionMillis, r.ElapsedMillis)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result["duration_complete"] != false {
		t.Fatal("崩溃区间必须明确标记时长不完整")
	}
	if result["unfinished_attempts"] != float64(1) {
		t.Fatal("必须报告一个未结束历史Attempt")
	}
}
