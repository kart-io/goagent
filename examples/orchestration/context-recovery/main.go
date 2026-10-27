// Run twice with the same directory. Immutable evidence survives process exit.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/kart-io/goagent/builder/contextpolicy"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

func run(dir string) (string, error) {
	file, err := durable.NewFileStore(dir)
	if err != nil {
		return "", err
	}
	store, err := contextpolicy.NewDurableResultStore(file)
	if err != nil {
		return "", err
	}
	policy, err := contextpolicy.NewOffload(contextpolicy.OffloadConfig{Store: store, Namespace: "order-evidence", MaxToolResultRunes: 3, ReadPageRunes: 2})
	if err != nil {
		return "", err
	}
	ctx := contextpolicy.WithResultScope(durable.WithJournal(durable.WithExecutionKey(context.Background(), "order/42/node"), file), "order/42")
	call := llm.ToolCall{ID: "lookup-1", Type: "function"}
	call.Function.Name = "lookup"
	call.Function.Arguments = "{}"
	turn, err := policy.BeforeTurn(ctx, contextpolicy.Turn{Messages: []llm.Message{{Role: "assistant", ToolCalls: []llm.ToolCall{call}}, {Role: "tool", ToolCallID: call.ID, Content: "甲乙丙丁戊"}}})
	if err != nil {
		return "", err
	}
	var ref struct {
		Key string `json:"context_result"`
	}
	if err = json.Unmarshal([]byte(turn.Messages[1].Content), &ref); err != nil {
		return "", err
	}
	out, err := policy.Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"key": ref.Key, "offset": float64(2)}})
	if err != nil {
		return "", err
	}
	return ref.Key + " " + fmt.Sprint(out.Result), nil
}
func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: context-recovery <store-directory>")
		os.Exit(2)
	}
	out, err := run(os.Args[1])
	if err != nil {
		panic(err)
	}
	fmt.Println(out)
}
