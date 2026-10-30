package document

import (
	"context"
	"strings"
	"testing"
)

// 下载失败时底层 *url.Error 的文本含完整 URL，query 中的凭据不得进入返回错误。
func TestLoaders_TransportErrorRedactsURL(t *testing.T) {
	const raw = "http://u:pw@127.0.0.1:1/doc.pdf?token=SECRET"
	loaders := map[string]func() error{
		"web": func() error {
			_, err := NewWebLoader(WebLoaderConfig{URL: raw}).Load(context.Background())
			return err
		},
		"pdf": func() error {
			_, err := NewPDFLoader(PDFLoaderConfig{Source: raw}).Load(context.Background())
			return err
		},
		"pdf 非法 URL": func() error {
			_, err := NewPDFLoader(PDFLoaderConfig{Source: "http://h/doc.pdf?token=SECRET\x7f"}).Load(context.Background())
			return err
		},
	}
	for name, load := range loaders {
		t.Run(name, func(t *testing.T) {
			err := load()
			if err == nil {
				t.Fatal("期望失败")
			}
			if s := err.Error(); strings.Contains(s, "SECRET") || strings.Contains(s, "pw@") {
				t.Errorf("未脱敏: %s", s)
			}
		})
	}
}
