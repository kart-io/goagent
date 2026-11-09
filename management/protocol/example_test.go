package protocol_test

import (
	"context"
	"net/http"

	mg "github.com/kart-io/goagent/management"
	a2aserver "github.com/kart-io/goagent/management/a2a"
	"github.com/kart-io/goagent/management/httpapi"
	"github.com/kart-io/goagent/management/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 同一个管理服务包含同一个 Store、Authorizer 和执行工厂；宿主另行启动其 Worker。
func Example() {
	mount := func(s *mg.Service, agentID, publicURL string, authenticate func(*http.Request) (mg.Actor, error)) (http.Handler, error) {
		a2a, err := a2aserver.New(s, a2aserver.Config{AgentID: agentID, URL: publicURL + "/a2a", ActorFromRequest: authenticate})
		if err != nil {
			return nil, err
		}
		httpAPI, err := httpapi.New(s, httpapi.Config{ActorFromRequest: authenticate})
		if err != nil {
			return nil, err
		}
		type actorKey struct{}
		mcpAPI, err := mcpserver.NewHTTPHandler(s, mcpserver.Config{
			Agents: []mcpserver.Agent{{ID: agentID, Name: "assistant", Description: "执行已发布的 Agent"}},
			ActorFromRequest: func(ctx context.Context, _ *mcp.CallToolRequest) (mg.Actor, error) {
				actor, _ := ctx.Value(actorKey{}).(mg.Actor)
				return actor, mg.ValidateActor(actor)
			},
		})
		if err != nil {
			return nil, err
		}
		mux := http.NewServeMux()
		mux.Handle("/.well-known/agent-card.json", a2a)
		mux.Handle("/a2a", a2a)
		mux.Handle("/v1/", httpAPI)
		// MCP 初始化、发现和调用均先认证；每次工具调用还会经过任务服务授权。
		mux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor, err := authenticate(r)
			if err != nil || mg.ValidateActor(actor) != nil {
				http.Error(w, "需要认证", http.StatusUnauthorized)
				return
			}
			mcpAPI.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, actor)))
		}))
		return mux, nil
	}
	_ = mount
}
