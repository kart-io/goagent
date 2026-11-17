package memory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// memory_replace 在同一提交中取代旧记录：旧记录失效且指向新记录，新记录继承钉住；旧记录不存在、已失效或 id 为空时不写入。
func TestMemoryReplace(t *testing.T) {
	cases := []struct {
		name       string
		oldPinned  bool
		forgetOld  bool
		replaces   string
		pinned     any // nil 表示不传
		wantCode   agentErrors.ErrorCode
		wantPinned bool
	}{
		{"继承钉住", true, false, "old", nil, "", true},
		{"显式不钉住仍继承", true, false, "old", false, "", true},
		{"旧记录未钉住", false, false, "old", nil, "", false},
		{"新记录自行钉住", false, false, "old", true, "", true},
		{"旧记录不存在", true, false, "missing", nil, agentErrors.CodeStoreNotFound, false},
		{"旧记录已遗忘", true, true, "old", nil, agentErrors.CodeConflict, false},
		{"id 为空", true, false, "", nil, agentErrors.CodeInvalidInput, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := vectorMemory(t)
			ctx := WithNamespace(context.Background(), testNS...)
			_, err := m.Remember(ctx, Record{ID: "old", Content: "用户只接受电话联系", Importance: 0.9, Pinned: tc.oldPinned})
			require.NoError(t, err)
			if tc.forgetOld {
				require.NoError(t, m.Forget(ctx, "old"))
			}
			args := map[string]any{"content": "用户改为只接受邮件联系", "id": tc.replaces}
			if tc.pinned != nil {
				args["pinned"] = tc.pinned
			}
			out, err := memoryToolNamed(m, constants.ToolMemoryReplace).Invoke(ctx, &interfaces.ToolInput{Args: args})
			_, s, rerr := m.read(ctx, testNS)
			require.NoError(t, rerr)
			if tc.wantCode != "" {
				require.True(t, agentErrors.IsCode(err, tc.wantCode), "错误码：%v", err)
				require.Len(t, s.Records, 1, "失败时不写入新记录")
				return
			}
			require.NoError(t, err)
			var res struct {
				ID string `json:"id"`
			}
			require.NoError(t, json.Unmarshal([]byte(out.Result.(string)), &res))
			fresh, old := s.Records[res.ID], s.Records["old"]
			require.True(t, fresh.Valid())
			require.Equal(t, tc.wantPinned, fresh.Pinned)
			require.False(t, old.Valid())
			require.Equal(t, ReasonSuperseded, old.InvalidReason)
			require.Equal(t, res.ID, old.SupersededBy)

			found, err := m.Recall(ctx, Query{Text: "用户联系方式", K: 5})
			require.NoError(t, err)
			for _, r := range found {
				require.NotEqual(t, "old", r.ID, "被取代的记录不再召回")
			}
		})
	}
}

func memoryToolNamed(m *Memory, name string) interfaces.Tool {
	for _, tool := range m.Tools() {
		if tool.Name() == name {
			return tool
		}
	}
	panic("没有 " + name)
}
