package practical

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
)

// TestWebScraperTool_ResultURLRedacted 结果中的 url 不得带出请求 URL 的 userinfo、query 与 fragment
func TestWebScraperTool_ResultURLRedacted(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{name: "成功路径", status: http.StatusOK},
		{name: "失败路径", status: http.StatusInternalServerError, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`<html><head><title>T</title></head><body>x</body></html>`))
			}))
			defer server.Close()

			rawURL := strings.Replace(server.URL, "http://", "http://user:pass@", 1) + "/page?token=secret#frag"
			output, err := NewWebScraperTool().Execute(context.Background(), &interfaces.ToolInput{
				Args: map[string]interface{}{"url": rawURL},
			})
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NotNil(t, output)

			got := output.Result.(map[string]interface{})["url"].(string)
			assert.Equal(t, server.URL+"/page", got)
			for _, secret := range []string{"user", "pass", "token", "secret", "frag"} {
				assert.NotContains(t, got, secret)
			}
		})
	}
}
