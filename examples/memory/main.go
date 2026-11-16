// memory 离线演示长期记忆的边界：命名空间隔离与删除、抽取管线、模型调用缓存一致性、召回预算与保留。
// 抽取与主模型都由 testing/mocks.ScriptedClient 按脚本回答，嵌入由确定性的假 Embedder 给出，不需要 API 密钥与网络。
package main

import (
	"context"
	"fmt"
	"io"
	"os"
)

func main() {
	if err := run(context.Background(), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, w io.Writer) error {
	root, err := os.MkdirTemp("", "memory-example-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(root) }()
	for _, s := range []struct {
		name string
		run  func(context.Context, io.Writer, string) error
	}{
		{"A 隔离与删除", isolation},
		{"B 抽取管线", extraction},
		{"C 缓存一致性", cacheConsistency},
		{"D 预算与保留", budgetRetention},
	} {
		_, _ = fmt.Fprintf(w, "== %s ==\n", s.name)
		if err := s.run(ctx, w, root); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		_, _ = fmt.Fprintf(w, "%s 完成\n", s.name)
	}
	return nil
}
