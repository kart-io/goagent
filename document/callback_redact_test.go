package document

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
)

// recordingCallback 记录 OnStart/OnEnd/OnError 收到的内容
type recordingCallback struct {
	*core.BaseCallback
	events []string
}

func (c *recordingCallback) OnStart(_ context.Context, input interface{}) error {
	c.events = append(c.events, fmt.Sprint(input))
	return nil
}

func (c *recordingCallback) OnEnd(_ context.Context, output interface{}) error {
	c.events = append(c.events, fmt.Sprint(output))
	return nil
}

func (c *recordingCallback) OnError(_ context.Context, err error) error {
	c.events = append(c.events, err.Error())
	return nil
}

func secretURL(t *testing.T, status int) (raw, redacted string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte("<html><body>x</body></html>"))
	}))
	t.Cleanup(server.Close)
	raw = strings.Replace(server.URL, "http://", "http://user:pass@", 1) + "/doc?token=secret#frag"
	return raw, server.URL + "/doc"
}

func assertCallbacksRedacted(t *testing.T, cb *recordingCallback, redacted string) {
	t.Helper()
	joined := strings.Join(cb.events, "\n")
	assert.Contains(t, joined, redacted)
	for _, secret := range []string{"user:pass", "token", "secret", "frag"} {
		assert.NotContains(t, joined, secret)
	}
}

// 回调收到的远程地址须脱敏；文档 Metadata["source"] 属于数据，保留原值
func TestWebLoaderCallbackURLRedacted(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			raw, redacted := secretURL(t, status)
			cb := &recordingCallback{BaseCallback: core.NewBaseCallback()}
			loader := NewWebLoader(WebLoaderConfig{URL: raw, CallbackManager: core.NewCallbackManager(cb)})

			docs, err := loader.Load(context.Background())
			if status == http.StatusOK {
				require.NoError(t, err)
				require.Len(t, docs, 1)
				assert.Equal(t, raw, docs[0].Metadata["source"])
			} else {
				require.Error(t, err)
			}
			assertCallbacksRedacted(t, cb, redacted)
		})
	}
}

func TestPDFLoaderCallbackURLRedacted(t *testing.T) {
	loads := map[string]func(*PDFLoader, context.Context) error{
		"Load": func(l *PDFLoader, ctx context.Context) error {
			_, err := l.Load(ctx)
			return err
		},
		"LoadByPage": func(l *PDFLoader, ctx context.Context) error {
			_, err := l.LoadByPage(ctx)
			return err
		},
	}
	for name, load := range loads {
		t.Run(name, func(t *testing.T) {
			raw, redacted := secretURL(t, http.StatusNotFound)
			cb := &recordingCallback{BaseCallback: core.NewBaseCallback()}
			loader := NewPDFLoader(PDFLoaderConfig{Source: raw, CallbackManager: core.NewCallbackManager(cb)})

			require.Error(t, load(loader, context.Background()))
			assertCallbacksRedacted(t, cb, redacted)
		})
	}
}

// 本地文件路径不做脱敏
func TestPDFLoaderCallbackLocalPathKept(t *testing.T) {
	path := t.TempDir() + "/a?b#c.pdf"
	cb := &recordingCallback{BaseCallback: core.NewBaseCallback()}
	loader := NewPDFLoader(PDFLoaderConfig{Source: path, CallbackManager: core.NewCallbackManager(cb)})

	_, err := loader.Load(context.Background())
	require.Error(t, err)
	assert.Contains(t, strings.Join(cb.events, "\n"), path)
}
