// stabilitysoak 持续运行有界离线混合负载；检查资源门槛，状态只输出到终端或本机内存接口。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type config struct {
	Duration, Interval, Sample, CycleTimeout time.Duration
	Workers, MaxGoroutines                   int
	MaxHeap, MaxDisk                         uint64
	TempRoot                                 string
}

type snapshot struct {
	Elapsed                                                  time.Duration
	Cycles, Requests, Effects, ReplayRequests, ReplayEffects uint64
	SampledPeakHeapBytes, SampledPeakDiskBytes               uint64
	SampledPeakGoroutines                                    int
	P95Millis, P99Millis                                     int64
}

type measurements struct {
	mu        sync.Mutex
	current   snapshot
	buckets   [8]uint64
	maxMillis int64
}

var latencyBounds = [...]int64{10, 25, 50, 100, 250, 500, 1000, 5000}

func (m *measurements) add(c cycleResult, elapsed time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current.Cycles++
	m.current.Requests += c.Requests
	m.current.Effects += c.Effects
	m.current.ReplayRequests += c.ReplayRequests
	m.current.ReplayEffects += c.ReplayEffects
	millis := elapsed.Milliseconds()
	if elapsed%time.Millisecond != 0 {
		millis++
	}
	m.maxMillis = max(m.maxMillis, millis)
	index := len(latencyBounds) - 1
	for i, bound := range latencyBounds {
		if elapsed <= time.Duration(bound)*time.Millisecond {
			index = i
			break
		}
	}
	m.buckets[index]++
}

func (m *measurements) read(start time.Time) snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	got := m.current
	got.Elapsed = time.Since(start)
	if got.Cycles == 0 {
		return got
	}
	var count uint64
	for i, n := range m.buckets {
		count += n
		bound := latencyBounds[i]
		if i == len(latencyBounds)-1 {
			bound = max(bound, m.maxMillis)
		}
		if got.P95Millis == 0 && count*100 >= got.Cycles*95 {
			got.P95Millis = bound
		}
		if got.P99Millis == 0 && count*100 >= got.Cycles*99 {
			got.P99Millis = bound
		}
	}
	return got
}

func (m *measurements) sample(directory string, cfg config) error {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	goroutines := runtime.NumGoroutine()
	var disk uint64
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, e := entry.Info()
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		disk += uint64(info.Size())
		return nil
	})
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.current.SampledPeakHeapBytes = max(m.current.SampledPeakHeapBytes, memory.HeapAlloc)
	m.current.SampledPeakDiskBytes = max(m.current.SampledPeakDiskBytes, disk)
	m.current.SampledPeakGoroutines = max(m.current.SampledPeakGoroutines, goroutines)
	m.mu.Unlock()
	if memory.HeapAlloc > cfg.MaxHeap {
		return fmt.Errorf("堆内存超过门槛：%d > %d", memory.HeapAlloc, cfg.MaxHeap)
	}
	if goroutines > cfg.MaxGoroutines {
		return fmt.Errorf("goroutine超过门槛：%d > %d", goroutines, cfg.MaxGoroutines)
	}
	if disk > cfg.MaxDisk {
		return fmt.Errorf("临时存储超过门槛：%d > %d", disk, cfg.MaxDisk)
	}
	return nil
}

func soak(ctx context.Context, cfg config, out io.Writer, publish func(snapshot)) (got snapshot, err error) {
	if cfg.Duration <= 0 || cfg.Workers < 1 || cfg.Workers > 128 || cfg.Interval <= 0 || cfg.Sample <= 0 || cfg.CycleTimeout <= 0 || cfg.MaxHeap < 1 || cfg.MaxDisk < 1 || cfg.MaxGoroutines < 1 {
		return got, fmt.Errorf("长稳时长、并发、间隔、期限和资源额度必须有效；并发不超过128")
	}
	if err = ctx.Err(); err != nil {
		return got, err
	}
	directory, err := os.MkdirTemp(cfg.TempRoot, "goagent-soak-state-")
	if err != nil {
		return got, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(directory)) }()
	start := time.Now()
	measure := &measurements{}
	if err = measure.sample(directory, cfg); err != nil {
		return got, err
	}
	loadCtx, stop := context.WithTimeout(ctx, cfg.Duration)
	defer stop()
	failures := make(chan error, 1)
	fail := func(e error) {
		select {
		case failures <- e:
		default:
		}
		stop()
	}
	var workers sync.WaitGroup
	var serial atomic.Uint64
	for range cfg.Workers {
		workers.Go(func() {
			for loadCtx.Err() == nil {
				name := filepath.Join(directory, fmt.Sprint(serial.Add(1)))
				cycleCtx, cancel := context.WithTimeout(ctx, cfg.CycleTimeout)
				before := time.Now()
				result, e := exercise(cycleCtx, name)
				cancel()
				e = errors.Join(e, os.RemoveAll(name))
				if e != nil {
					fail(e)
					return
				}
				measure.add(result, time.Since(before))
				timer := time.NewTimer(cfg.Interval)
				select {
				case <-loadCtx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		})
	}
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	lastSample := start
	ticker := time.NewTicker(cfg.Sample)
	defer ticker.Stop()
loop:
	for {
		select {
		case <-done:
			break loop
		case <-ticker.C:
			if time.Since(lastSample) > max(2*cfg.Sample, cfg.CycleTimeout) {
				fail(fmt.Errorf("采样中断过久，不能认定为连续长稳运行"))
			}
			lastSample = time.Now()
			if e := measure.sample(directory, cfg); e != nil {
				fail(e)
			}
			got = measure.read(start)
			if publish != nil {
				publish(got)
			}
			if _, e := fmt.Fprintf(out, "耗时=%s 周期=%d 请求=%d 效果=%d 堆采样峰值=%dMiB goroutine采样峰值=%d P95≤%dms P99≤%dms\n", got.Elapsed.Round(time.Second), got.Cycles, got.Requests, got.Effects, got.SampledPeakHeapBytes>>20, got.SampledPeakGoroutines, got.P95Millis, got.P99Millis); e != nil {
				fail(e)
			}
		}
	}
	got = measure.read(start)
	select {
	case err = <-failures:
		return got, err
	default:
	}
	if err = ctx.Err(); err != nil {
		return got, err
	}
	if time.Since(lastSample) > max(2*cfg.Sample, cfg.CycleTimeout) {
		return got, fmt.Errorf("采样中断过久，不能认定为连续长稳运行")
	}
	if got.Cycles == 0 || got.Elapsed < cfg.Duration {
		return got, fmt.Errorf("未完成声明的长稳时长或没有有效负载")
	}
	if err = measure.sample(directory, cfg); err != nil {
		return got, err
	}
	got = measure.read(start)
	return got, nil
}

func main() {
	cfg := config{}
	flag.DurationVar(&cfg.Duration, "duration", 24*time.Hour, "实际运行时长")
	flag.IntVar(&cfg.Workers, "workers", 4, "独立并发会话")
	flag.DurationVar(&cfg.Interval, "interval", time.Second, "每个工作者的周期间隔")
	flag.DurationVar(&cfg.Sample, "sample", time.Minute, "采样间隔")
	flag.DurationVar(&cfg.CycleTimeout, "cycle-timeout", 5*time.Second, "混合周期的执行期限")
	flag.Uint64Var(&cfg.MaxHeap, "max-heap", 256<<20, "堆内存上限字节")
	flag.IntVar(&cfg.MaxGoroutines, "max-goroutines", 128, "goroutine数量上限")
	flag.Uint64Var(&cfg.MaxDisk, "max-disk", 16<<20, "临时存储上限字节")
	address := flag.String("status-address", "", "可选本机状态地址，例如127.0.0.1:18763；结果保留在内存直到停止")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	exit := 0
	if err := execute(ctx, cancel, cfg, *address, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		exit = 1
	}
	if exit != 0 {
		os.Exit(exit)
	}
}
