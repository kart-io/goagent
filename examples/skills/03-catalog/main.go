// 演示普通 GoAgent 应用的三级 Skills 加载，无需 Harness 或模型密钥。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/skills"
	"github.com/kart-io/goagent/tools"
	skilltool "github.com/kart-io/goagent/tools/skill"
)

func main() {
	dir := flag.String("dir", "examples/skills/03-catalog/fixtures", "调用方指定的技能目录")
	flag.Parse()
	if err := run(*dir); err != nil {
		log.Fatal(err)
	}
}

func run(dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	ctx := context.Background()
	catalog, err := skills.Discover(ctx, root)
	if err != nil {
		return err
	}
	defer func() { _ = catalog.Close() }()
	for _, m := range catalog.List() {
		fmt.Printf("%s@%s: %s\n", m.Name, m.Version, m.Description)
	}
	loaded, err := catalog.Load(ctx, "faq")
	if err != nil {
		return err
	}
	fmt.Println(loaded.Body)
	fmt.Printf("资源：%v\n", loaded.Files)
	// 同一个 Catalog 也能注册到 Builder.WithTools，生命周期由宿主维护。
	tool := skilltool.New(catalog)
	out, err := tool.Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"name": "faq", "file": "references/refunds.md"}})
	if err != nil {
		return err
	}
	if message, rejected := tools.RejectedResult(out); rejected {
		return fmt.Errorf("load_skill rejected: %s", message)
	}
	if !out.Success {
		return fmt.Errorf("load_skill failed: %s", out.Error)
	}
	data, err := json.Marshal(out.Result)
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}
