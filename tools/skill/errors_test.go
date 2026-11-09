package skill

import (
	"context"
	"errors"
	"io/fs"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
	agenterrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/skills"
	"github.com/kart-io/goagent/tools"
)

func TestUnclassifiedReadErrorsPreserveCause(t *testing.T) {
	for _, cause := range []error{syscall.EMFILE, syscall.ENFILE, syscall.EIO, errors.New("unclassified failure")} {
		for _, file := range []string{"", "ref.txt"} {
			err := &fs.PathError{Op: "open", Path: "ref.txt", Err: cause}
			out, got := loadError(context.Background(), &skills.Catalog{}, tools.NewToolErrorResponse(constants.ToolLoadSkill), "demo", file, err)
			require.ErrorIs(t, got, cause)
			_, rejected := tools.RejectedResult(out)
			require.False(t, rejected, "incomplete I/O must not be durably recorded as a business decision")
		}
	}
}

func TestJoinedRequestAndIOErrorsRemainExecutionFailures(t *testing.T) {
	for _, requestErr := range []error{fs.ErrNotExist, fs.ErrPermission, fs.ErrInvalid, agenterrors.New(agenterrors.CodeInvalidInput, "file byte limit exceeded")} {
		for _, err := range []error{errors.Join(requestErr, syscall.EIO), errors.Join(syscall.EIO, requestErr), agenterrors.Wrap(syscall.EIO, agenterrors.CodeInvalidInput, "validation and I/O failure")} {
			for _, file := range []string{"", "ref.txt"} {
				out, got := loadError(context.Background(), &skills.Catalog{}, tools.NewToolErrorResponse(constants.ToolLoadSkill), "missing", file, err)
				require.ErrorIs(t, got, syscall.EIO)
				_, rejected := tools.RejectedResult(out)
				require.False(t, rejected)
			}
		}
	}
}

func TestInstructionLimitsAreCompletedRejections(t *testing.T) {
	for _, reason := range []string{"skill resource depth limit exceeded", "skill directory entry limit exceeded"} {
		err := agenterrors.New(agenterrors.CodeInvalidInput, reason)
		out, got := loadError(context.Background(), &skills.Catalog{}, tools.NewToolErrorResponse(constants.ToolLoadSkill), "demo", "", err)
		require.NoError(t, got)
		message, rejected := tools.RejectedResult(out)
		require.True(t, rejected, "已知枚举限制必须形成完成的拒绝，供持久执行返回模型反馈")
		require.Contains(t, message, reason)
	}
}
