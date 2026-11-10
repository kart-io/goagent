package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
)

// Agent Card 只能把调用引向发现地址同源的接口，其他源的接口被忽略。
func TestConnectOnlyUsesSameOriginInterfaces(t *testing.T) {
	var server *httptest.Server
	tests := []struct {
		name       string
		interfaces func() []*a2a.AgentInterface
		ok         bool
	}{
		{"只有其他源", func() []*a2a.AgentInterface {
			return []*a2a.AgentInterface{a2a.NewAgentInterface("http://169.254.169.254/rpc", a2a.TransportProtocolJSONRPC)}
		}, false},
		{"同源与其他源并存", func() []*a2a.AgentInterface {
			return []*a2a.AgentInterface{
				a2a.NewAgentInterface("http://169.254.169.254/rpc", a2a.TransportProtocolJSONRPC),
				a2a.NewAgentInterface(server.URL+"/rpc", a2a.TransportProtocolJSONRPC),
			}
		}, true},
		{"协议不同", func() []*a2a.AgentInterface {
			return []*a2a.AgentInterface{a2a.NewAgentInterface("https"+server.URL[len("http"):]+"/rpc", a2a.TransportProtocolJSONRPC)}
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				data, _ := json.Marshal(&a2a.AgentCard{Name: "remote", SupportedInterfaces: tt.interfaces()})
				_, _ = w.Write(data)
			}))
			defer server.Close()
			client, err := Connect(context.Background(), server.URL, server.Client())
			if !tt.ok {
				require.ErrorContains(t, err, "同源")
				return
			}
			require.NoError(t, err)
			require.NoError(t, client.Destroy())
		})
	}
}
