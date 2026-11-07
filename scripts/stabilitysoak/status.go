package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/kart-io/goagent/codec/json"
	"io"
	"net"
	"net/http"
	"runtime/debug"
	"sync"
	"time"
)

// execute 的可选状态接口仅监听本机，不持久化运行报告。完成后保持结果，供下一轮检查。
func execute(ctx context.Context, cancel context.CancelFunc, cfg config, address string, out io.Writer) error {
	var mu sync.Mutex
	state := struct {
		Phase    string
		Revision string
		Started  time.Time
		Target   time.Duration
		Result   snapshot
		Error    string
	}{Phase: "running", Revision: sourceRevision(), Started: time.Now().UTC(), Target: cfg.Duration}
	update := func(got snapshot) { mu.Lock(); state.Result = got; mu.Unlock() }
	if address != "" {
		host, _, err := net.SplitHostPort(address)
		if err != nil || (host != "127.0.0.1" && host != "::1") {
			return fmt.Errorf("状态接口必须绑定本机回环地址")
		}
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return err
		}
		mux := http.NewServeMux()
		mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			copy := state
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.Encode(w, copy)
		})
		mux.HandleFunc("POST /stop", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted); cancel() })
		server := &http.Server{Handler: mux, ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
		serveErr := make(chan error, 1)
		go func() { serveErr <- server.Serve(listener) }()
		defer func() {
			shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			_ = server.Shutdown(shutdown)
		}()
		go func() {
			select {
			case err := <-serveErr:
				if !errors.Is(err, http.ErrServerClosed) {
					cancel()
				}
			case <-ctx.Done():
			}
		}()
	}
	got, err := soak(ctx, cfg, out, update)
	mu.Lock()
	state.Result = got
	if err != nil {
		state.Phase = "failed"
		state.Error = err.Error()
	} else {
		state.Phase = "passed"
	}
	mu.Unlock()
	if address != "" && ctx.Err() == nil {
		<-ctx.Done()
	}
	return err
}

func sourceRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	revision, modified := "unknown", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if modified {
		revision += "-dirty"
	}
	return revision
}
