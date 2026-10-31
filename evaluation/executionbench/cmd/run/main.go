// run 输出无模型请求的离线预算开销矩阵。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/kart-io/goagent/evaluation/executionbench"
)

type output struct {
	Timestamp   string                  `json:"timestamp"`
	Hostname    string                  `json:"hostname"`
	GOOS        string                  `json:"goos"`
	GOARCH      string                  `json:"goarch"`
	GoVersion   string                  `json:"goVersion"`
	CPUs        int                     `json:"cpus"`
	GOMAXPROCS  int                     `json:"gomaxprocs"`
	Timeout     string                  `json:"timeout"`
	Limitations string                  `json:"limitations"`
	Reports     []executionbench.Report `json:"reports"`
	Error       string                  `json:"error,omitempty"`
}

func main() { os.Exit(run()) }
func run() int {
	sizes := flag.String("operations", "10,100,1000", "以逗号分隔的固定操作数")
	modes := flag.String("modes", "none,memory,file", "以逗号分隔的预算模式")
	timeout := flag.Duration("timeout", 10*time.Minute, "整个矩阵的截止时间，必须为正数")
	tempDir := flag.String("temp-dir", "", "file模式临时子目录的父目录")
	flag.Parse()
	host, _ := os.Hostname()
	out := output{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Hostname: host, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, GoVersion: runtime.Version(), CPUs: runtime.NumCPU(), GOMAXPROCS: runtime.GOMAXPROCS(0), Timeout: timeout.String(), Limitations: "离线无副作用工具与固定合成usage；未调用模型，不代表真实模型效果、费用或生产试运行；堆和goroutine为进程采样，不证明无泄漏"}
	var runErr error
	counts := []int{}
	for _, raw := range strings.Split(*sizes, ",") {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			runErr = fmt.Errorf("operations必须为正整数列表")
			break
		}
		counts = append(counts, n)
	}
	selectedModes := strings.Split(*modes, ",")
	for _, mode := range selectedModes {
		if mode != "none" && mode != "memory" && mode != "file" {
			runErr = fmt.Errorf("mode必须是none、memory或file")
			break
		}
	}
	if *timeout <= 0 {
		runErr = fmt.Errorf("timeout必须为正数")
	}
	if flag.NArg() != 0 {
		runErr = fmt.Errorf("不接受位置参数")
	}
	if runErr == nil {
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
	outer:
		for _, n := range counts {
			for _, mode := range selectedModes {
				cfg := executionbench.Config{Mode: mode, Operations: n}
				if mode == "file" {
					cfg.TempDir = *tempDir
				}
				report, err := executionbench.Run(ctx, cfg)
				out.Reports = append(out.Reports, report)
				if err != nil {
					runErr = err
					break outer
				}
			}
		}
	}
	if runErr != nil {
		out.Error = runErr.Error()
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if runErr != nil {
		return 1
	}
	return 0
}
