package protocol_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
	a2aserver "github.com/kart-io/goagent/management/a2a"
	"github.com/kart-io/goagent/management/httpapi"
	"github.com/kart-io/goagent/management/internal/managementtest"
	"github.com/kart-io/goagent/management/mcpserver"
)

func TestRemoteEntryPointsRequireTaskBudget(t *testing.T) {
	entries := []struct {
		name  string
		build func(*mg.Service) error
	}{
		{"httpapi", func(s *mg.Service) error {
			_, err := httpapi.New(s, httpapi.Config{ActorFromRequest: func(*http.Request) (mg.Actor, error) { return mg.Actor{}, nil }})
			return err
		}},
		{"a2a", func(s *mg.Service) error {
			_, err := a2aserver.New(s, a2aserver.Config{AgentID: "agent", URL: "https://example.com/a2a", ActorFromRequest: func(*http.Request) (mg.Actor, error) { return mg.Actor{}, nil }})
			return err
		}},
		{"mcpserver", func(s *mg.Service) error {
			_, err := mcpserver.New(s, mcpserver.Config{Agents: []mcpserver.Agent{{ID: "agent", Name: "assistant"}}, ActorFromRequest: func(context.Context, *mcp.CallToolRequest) (mg.Actor, error) {
				return mg.Actor{}, nil
			}})
			return err
		}},
	}
	configs := []struct {
		name      string
		budget    mg.TaskBudget
		unlimited bool
		wantErr   bool
	}{
		{"未配置预算也未显式放开时拒绝启动", mg.TaskBudget{}, false, true},
		{"配置了预算时可以启动", mg.TaskBudget{MaxModelRequests: 100, Timeout: time.Hour}, false, false},
		{"只配置单项上限也可以启动", mg.TaskBudget{Timeout: time.Hour}, false, false},
		{"显式放开时允许零预算", mg.TaskBudget{}, true, false},
	}
	for _, e := range entries {
		for _, c := range configs {
			t.Run(e.name+"/"+c.name, func(t *testing.T) {
				f := managementtest.NewFixture(t)
				cfg := f.Config()
				cfg.DefaultTaskBudget = c.budget
				cfg.UnlimitedRemoteTaskBudget = c.unlimited
				s, err := mg.New(cfg)
				require.NoError(t, err)

				err = e.build(s)
				if !c.wantErr {
					require.NoError(t, err)
					return
				}
				require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
				require.ErrorContains(t, err, "DefaultTaskBudget")
			})
		}
	}
}

func TestValidateRemoteTaskBudgetNilService(t *testing.T) {
	var s *mg.Service
	require.ErrorIs(t, s.ValidateRemoteTaskBudget(), agentErrors.ErrManagementInvalid)
}
