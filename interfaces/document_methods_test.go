package interfaces

import (
	"strings"
	"testing"
)

func TestDocumentClone(t *testing.T) {
	orig := &Document{
		ID:          "d1",
		PageContent: "body",
		Metadata:    map[string]interface{}{"k": "v"},
		Embedding:   []float32{1, 2},
		Score:       0.5,
	}
	c := orig.Clone()
	if c.ID != "d1" || c.PageContent != "body" || c.Score != 0.5 || c.Metadata["k"] != "v" || len(c.Embedding) != 2 {
		t.Fatalf("克隆结果与原文档不一致: %+v", c)
	}

	c.Metadata["k"] = "changed"
	c.Embedding[0] = 9
	if orig.Metadata["k"] != "v" || orig.Embedding[0] != 1 {
		t.Fatal("修改克隆体不应影响原文档")
	}

	empty := (&Document{}).Clone()
	if empty.Metadata == nil || empty.Embedding == nil {
		t.Fatal("空文档克隆后 Metadata 与 Embedding 应为非 nil 的空值")
	}
}

func TestDocumentMetadataAccess(t *testing.T) {
	d := &Document{}
	if _, ok := d.GetMetadata("k"); ok {
		t.Fatal("Metadata 为 nil 时不应取到值")
	}
	d.SetMetadata("k", 1)
	if v, ok := d.GetMetadata("k"); !ok || v != 1 {
		t.Fatalf("GetMetadata = %v, %v，期望 1, true", v, ok)
	}
	if _, ok := d.GetMetadata("missing"); ok {
		t.Fatal("不存在的键不应取到值")
	}
	d.SetMetadata("k", 2)
	if v, _ := d.GetMetadata("k"); v != 2 {
		t.Fatalf("覆盖写后 GetMetadata = %v，期望 2", v)
	}
}

func TestDocumentString(t *testing.T) {
	cases := []struct {
		name string
		doc  Document
		want string
	}{
		{
			name: "无元数据",
			doc:  Document{ID: "d1", Score: 0.25, PageContent: "hi"},
			want: "Document(ID=d1, Score=0.2500, Content='hi')",
		},
		{
			name: "元数据按键排序",
			doc:  Document{ID: "d2", Metadata: map[string]interface{}{"b": 2, "a": "x"}, PageContent: "c"},
			want: "Document(ID=d2, Score=0.0000, Metadata={a: x, b: 2}, Content='c')",
		},
		{
			name: "内容超过 50 字节截断",
			doc:  Document{ID: "d3", PageContent: strings.Repeat("a", 51)},
			want: "Document(ID=d3, Score=0.0000, Content='" + strings.Repeat("a", 50) + "...')",
		},
		{
			name: "内容恰好 50 字节不截断",
			doc:  Document{ID: "d4", PageContent: strings.Repeat("a", 50)},
			want: "Document(ID=d4, Score=0.0000, Content='" + strings.Repeat("a", 50) + "')",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.doc.String(); got != c.want {
				t.Fatalf("String() = %q\n期望 %q", got, c.want)
			}
		})
	}
}
