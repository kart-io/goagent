package main

import (
	"context"
	"github.com/kart-io/goagent/codec/json"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testConfig() config {
	return config{Duration: 50 * time.Millisecond, Workers: 2, Interval: time.Millisecond, Sample: 10 * time.Millisecond, CycleTimeout: 5 * time.Second, MaxHeap: 256 << 20, MaxGoroutines: 128, MaxDisk: 16 << 20}
}

func TestExerciseChecksRealExecutionAndRecovery(t *testing.T) {
	got, err := exercise(context.Background(), t.TempDir())
	require.NoError(t, err)
	require.EqualValues(t, 2, got.Requests)
	require.EqualValues(t, 2, got.Effects)
	require.Zero(t, got.ReplayRequests)
	require.Zero(t, got.ReplayEffects)
}

func TestSoakRunsMixedLoadAndRemovesState(t *testing.T) {
	cfg := testConfig()
	cfg.TempRoot = t.TempDir()
	got, err := soak(context.Background(), cfg, io.Discard, nil)
	require.NoError(t, err)
	require.GreaterOrEqual(t, got.Elapsed, cfg.Duration)
	require.Greater(t, got.Cycles, uint64(0))
	require.Equal(t, got.Cycles*2, got.Requests)
	require.Equal(t, got.Cycles*2, got.Effects)
	require.Zero(t, got.ReplayRequests)
	require.Zero(t, got.ReplayEffects)
	entries, err := os.ReadDir(cfg.TempRoot)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestSoakDoesNotTreatParentCancellationAsSuccess(t *testing.T) {
	cfg := testConfig()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := soak(ctx, cfg, io.Discard, nil)
	require.ErrorIs(t, err, context.Canceled)
}

func TestSoakRejectsInvalidLimits(t *testing.T) {
	for _, change := range []func(*config){
		func(c *config) { c.Duration = 0 }, func(c *config) { c.Workers = 0 },
		func(c *config) { c.Interval = 0 }, func(c *config) { c.Sample = 0 },
		func(c *config) { c.CycleTimeout = 0 }, func(c *config) { c.MaxHeap = 0 },
		func(c *config) { c.MaxGoroutines = 0 }, func(c *config) { c.MaxDisk = 0 },
	} {
		cfg := testConfig()
		change(&cfg)
		_, err := soak(context.Background(), cfg, io.Discard, nil)
		require.Error(t, err)
	}
}

func TestSoakEnforcesResourceLimit(t *testing.T) {
	cfg := testConfig()
	cfg.MaxHeap = 1
	_, err := soak(context.Background(), cfg, io.Discard, nil)
	require.ErrorContains(t, err, "堆内存")
}

func TestStatusKeepsCompletedResultInMemory(t *testing.T) {
	reserve, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := reserve.Addr().String()
	require.NoError(t, reserve.Close())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- execute(ctx, cancel, testConfig(), address, io.Discard) }()
	client := &http.Client{Timeout: time.Second}
	require.Eventually(t, func() bool {
		response, e := client.Get("http://" + address + "/status")
		if e != nil {
			return false
		}
		defer response.Body.Close()
		var status struct {
			Phase  string
			Result snapshot
			Error  string
		}
		if json.Decode(response.Body, &status) != nil {
			return false
		}
		return status.Phase == "passed" && status.Error == "" && status.Result.Cycles > 0
	}, 5*time.Second, 10*time.Millisecond)
	response, err := client.Post("http://"+address+"/stop", "text/plain", nil)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusAccepted, response.StatusCode)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("状态服务未停止")
	}
}

func TestStatusRejectsPublicListener(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.ErrorContains(t, execute(ctx, cancel, testConfig(), "0.0.0.0:0", io.Discard), "回环地址")
}

func TestLatencyBoundsDoNotUnderreportSlowCycles(t *testing.T) {
	measurements := &measurements{}
	empty := measurements.read(time.Now())
	require.Zero(t, empty.P95Millis)
	require.Zero(t, empty.P99Millis)
	measurements.add(cycleResult{}, 6100*time.Millisecond)
	got := measurements.read(time.Now())
	require.GreaterOrEqual(t, got.P95Millis, int64(6100))
	require.GreaterOrEqual(t, got.P99Millis, int64(6100))
}
