package contextpolicy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/testing/panictest"
	"github.com/stretchr/testify/require"
)

// 子进程角色：put 等父进程放行后依次写入 RUNS 个运行、每个运行 childRefs 条证据并逐条报告结果；
// hold 写入一条证据后在持有闸门共享锁与记录锁时阻塞，等待被父进程杀掉。
const (
	childRoleEnv   = "GOAGENT_RESULT_CHILD"
	childDirEnv    = "GOAGENT_RESULT_DIR"
	childPrefixEnv = "GOAGENT_RESULT_PREFIX"
	childRunsEnv   = "GOAGENT_RESULT_RUNS"
	childRefs      = 4
	childLine      = "@"
)

func runName(prefix string, i int) string { return fmt.Sprintf("%s-%d", prefix, i) }

func childRef(run string, j int) (Ref, []byte) {
	data := []byte(fmt.Sprintf("%s/%d", run, j))
	return NewRef(run, "p", strconv.Itoa(j), data), data
}

// holdStore 在证据完整写入并留下半截临时文件后阻塞，模拟写入中途进程被杀。
type holdStore struct {
	*durable.FileStore
	dir string
}

func (h holdStore) Scope(name string) durable.Store {
	return holdScope{Store: h.FileStore.Scope(name), dir: filepath.Join(h.dir, "scopes", digest([]byte(name)))}
}

type holdScope struct {
	durable.Store
	dir string
}

func (h holdScope) Save(ctx context.Context, id string, snapshot *durable.Snapshot) error {
	if err := h.Store.Save(ctx, id, snapshot); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(h.dir, ".checkpoint-partial"), []byte("{\"ha"), 0600); err != nil {
		return err
	}
	fmt.Println(childLine + "ready")
	time.Sleep(time.Minute)
	return errors.New("子进程未被杀掉")
}

// TestResultStoreChildProcess 是跨进程测试的子进程入口，直接运行时跳过。
func TestResultStoreChildProcess(t *testing.T) {
	role := os.Getenv(childRoleEnv)
	if role == "" {
		t.Skip("仅作为子进程运行")
	}
	dir, prefix := os.Getenv(childDirEnv), os.Getenv(childPrefixEnv)
	file, err := durable.NewFileStore(dir)
	require.NoError(t, err)
	switch role {
	case "put":
		runs, err := strconv.Atoi(os.Getenv(childRunsEnv))
		require.NoError(t, err)
		s, err := NewDurableResultStore(file)
		require.NoError(t, err)
		fmt.Println(childLine + "ready")
		_, err = bufio.NewReader(os.Stdin).ReadString('\n')
		require.NoError(t, err)
		for i := range runs {
			run := runName(prefix, i)
			ctx := WithResultScope(context.Background(), run)
			for j := range childRefs {
				ref, data := childRef(run, j)
				outcome := "ok"
				if err := s.PutIfAbsent(ctx, ref, data); errors.Is(err, ErrReleased) {
					outcome = "released"
				} else if err != nil {
					outcome = "error " + strings.ReplaceAll(err.Error(), "\n", " ")
				}
				fmt.Printf("%s%s %d %s\n", childLine, run, j, outcome)
			}
		}
	case "hold":
		s, err := NewDurableResultStore(holdStore{FileStore: file, dir: dir})
		require.NoError(t, err)
		run := runName(prefix, 0)
		ref, data := childRef(run, 0)
		t.Fatalf("写入不应返回：%v", s.PutIfAbsent(WithResultScope(context.Background(), run), ref, data))
	default:
		t.Fatalf("未知子进程角色 %q", role)
	}
}

type child struct {
	cmd   *exec.Cmd
	ctx   context.Context
	stdin io.WriteCloser
	lines chan string
	// done 在子进程被回收后关闭；之后才能读取 err 与 stderr。
	done   chan struct{}
	err    error
	stderr strings.Builder
}

const childTimeout = 2 * time.Minute

func startChild(t *testing.T, role, dir, prefix string, runs int) *child {
	t.Helper()
	return startChildTimeout(t, role, dir, prefix, runs, panictest.ChildTimeout(t, childTimeout))
}

// startChildTimeout 启动子进程，超过 timeout 未退出时杀掉；测试结束时子进程一定已被杀掉并回收。
func startChildTimeout(t *testing.T, role, dir, prefix string, runs int, timeout time.Duration) *child {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	c := &child{ctx: ctx, lines: make(chan string, 4096), done: make(chan struct{})}
	c.cmd = exec.CommandContext(ctx, os.Args[0], "-test.run=^TestResultStoreChildProcess$")
	c.cmd.Env = append(os.Environ(), childRoleEnv+"="+role, childDirEnv+"="+dir, childPrefixEnv+"="+prefix, childRunsEnv+"="+strconv.Itoa(runs))
	c.cmd.Stderr = &c.stderr
	c.cmd.WaitDelay = time.Second
	var err error
	c.stdin, err = c.cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := c.cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, c.cmd.Start())
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if line, ok := strings.CutPrefix(scanner.Text(), childLine); ok {
				c.lines <- line
			}
		}
		close(c.lines)
		// StdoutPipe 要求读完输出后才能 Wait。
		c.err = c.cmd.Wait()
		close(c.done)
	}()
	t.Cleanup(func() {
		cancel()
		<-c.done
	})
	return c
}

// describe 等子进程被回收后描述其退出情况，供失败信息定位；会阻塞到子进程退出，只能在确认失败后调用。
func (c *child) describe() string {
	<-c.done
	if c.ctx.Err() != nil {
		return fmt.Sprintf("子进程超时被杀掉（%v）：%v\nstderr: %s", c.ctx.Err(), c.err, c.stderr.String())
	}
	return fmt.Sprintf("子进程退出：%v\nstderr: %s", c.err, c.stderr.String())
}

// expect 读取子进程的下一条报告行。
func (c *child) expect(t *testing.T, want string) {
	t.Helper()
	select {
	case line, ok := <-c.lines:
		if !ok {
			t.Fatalf("子进程提前退出，未报告 %q：%s", want, c.describe())
		}
		require.Equal(t, want, line)
	case <-time.After(30 * time.Second):
		t.Fatalf("等待子进程报告 %q 超时", want)
	}
}

// exit 读完剩余报告行，等待子进程退出并被回收，返回报告行与退出错误；子进程超时时会被杀掉，因此不会无限等待。
func (c *child) exit() ([]string, error) {
	var lines []string
	for line := range c.lines {
		lines = append(lines, line)
	}
	<-c.done
	return lines, c.err
}

// wait 读完剩余报告行并断言子进程正常退出。
func (c *child) wait(t *testing.T) []string {
	t.Helper()
	lines, err := c.exit()
	if err != nil {
		t.Fatalf("子进程未正常退出：%s", c.describe())
	}
	return lines
}

// 父进程 Release 之后，另一个进程对同一运行键的 PutIfAbsent（新引用与已写过的引用）都被拒绝且不留残留。
func TestResultStoreProcessPutAfterRelease(t *testing.T) {
	dir := t.TempDir()
	s := newFileResultStore(t, dir)
	run := runName("run", 0)
	ctx := WithResultScope(context.Background(), run)
	ref, data := childRef(run, 0)
	require.NoError(t, s.PutIfAbsent(ctx, ref, data))
	require.NoError(t, s.Release(ctx, run))

	c := startChild(t, "put", dir, "run", 1)
	c.expect(t, "ready")
	_, err := io.WriteString(c.stdin, "go\n")
	require.NoError(t, err)
	lines := c.wait(t)
	require.Len(t, lines, childRefs)
	for j, line := range lines {
		require.Equal(t, fmt.Sprintf("%s %d released", run, j), line)
	}
	requireOnlyTombstones(t, dir, 1)
}

// 两个子进程与父进程并发：子进程按顺序逐个运行写入，父进程逆序逐个运行 Release。每条写入要么成功（随后被 Release 删除）、
// 要么被拒绝；同一运行内一旦被拒绝，之后的写入都被拒绝；最终磁盘上只剩每个运行一个墓碑。
func TestResultStoreProcessConcurrentRelease(t *testing.T) {
	const runs = 40
	dir := t.TempDir()
	s := newFileResultStore(t, dir)
	children := []*child{startChild(t, "put", dir, "race", runs), startChild(t, "put", dir, "race", runs)}
	for _, c := range children {
		c.expect(t, "ready")
	}
	release := func(i int) {
		run := runName("race", i)
		require.NoError(t, s.Release(WithResultScope(context.Background(), run), run))
	}
	// 两种结果都必须确定出现，不能依赖调度：放行前先释放最后一个运行，子进程写到它时必然被拒绝；
	// 等每个子进程报告第一条结果后才开始其余释放，第一个运行的首条写入必然成功。
	release(runs - 1)
	first := make([]string, len(children))
	for i, c := range children {
		_, err := io.WriteString(c.stdin, "go\n")
		require.NoError(t, err)
		select {
		case line, ok := <-c.lines:
			if !ok {
				t.Fatalf("子进程提前退出：%s", c.describe())
			}
			first[i] = line
		case <-time.After(30 * time.Second):
			t.Fatal("等待子进程第一条写入结果超时")
		}
	}
	// 其余运行逆序释放，与子进程的顺序写入在中途相遇，相遇处同一运行上的写入与 Release 真正并发。
	for i := runs - 2; i >= 0; i-- {
		release(i)
	}
	counts := map[string]int{}
	for i, c := range children {
		lines := append([]string{first[i]}, c.wait(t)...)
		require.Len(t, lines, runs*childRefs)
		rejected := map[string]bool{}
		for _, line := range lines {
			fields := strings.Fields(line)
			require.Len(t, fields, 3, line)
			run, outcome := fields[0], fields[2]
			require.Contains(t, []string{"ok", "released"}, outcome, line)
			if outcome == "ok" {
				require.False(t, rejected[run], "被拒绝之后不应再写入成功：%s", line)
			} else {
				rejected[run] = true
			}
			counts[outcome]++
		}
	}
	t.Logf("写入结果分布：%v", counts)
	require.Positive(t, counts["ok"], "应覆盖释放前写入成功的路径")
	require.Positive(t, counts["released"], "应覆盖释放后写入被拒绝的路径")
	requireOnlyTombstones(t, dir, runs)
	for i := range runs {
		run := runName("race", i)
		for j := range childRefs {
			ref, _ := childRef(run, j)
			_, err := s.Get(WithResultScope(context.Background(), run), ref)
			require.ErrorIs(t, err, ErrReleased)
		}
	}
}

// 子进程持有闸门共享锁写入时，父进程的 Release 必须等待（跨进程互斥）；子进程被 SIGKILL 后锁随进程释放，
// Release 立即完成并清理证据与半截临时文件，之后的写入被拒绝。
func TestResultStoreProcessKilledWriter(t *testing.T) {
	dir := t.TempDir()
	s := newFileResultStore(t, dir)
	run := runName("killed", 0)
	ctx := WithResultScope(context.Background(), run)
	c := startChild(t, "hold", dir, "killed", 1)
	c.expect(t, "ready")

	scopeDir := filepath.Join(dir, "scopes", digest([]byte(run)))
	short, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, s.Release(short, run), context.DeadlineExceeded, "子进程持锁期间 Release 等待")
	gate, err := os.ReadFile(scopeDir + ".gate")
	require.NoError(t, err)
	require.Empty(t, gate, "等待期间没有写入墓碑")
	_, err = os.Stat(filepath.Join(scopeDir, ".checkpoint-partial"))
	require.NoError(t, err)

	require.NoError(t, c.cmd.Process.Kill())
	_, err = c.exit()
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.False(t, exit.Success())

	start := time.Now()
	released, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	require.NoError(t, s.Release(released, run), "死亡进程的锁已释放")
	require.Less(t, time.Since(start), time.Second)
	requireOnlyTombstones(t, dir, 1)
	ref, data := childRef(run, 1)
	require.ErrorIs(t, s.PutIfAbsent(ctx, ref, data), ErrReleased)
	requireOnlyTombstones(t, dir, 1)
}

// 子进程卡住时按时限被杀掉并回收，读取报告行不会无限等待，失败信息能指出超时。
func TestResultStoreProcessChildTimeout(t *testing.T) {
	c := startChildTimeout(t, "hold", t.TempDir(), "timeout", 1, 2*time.Second)
	c.expect(t, "ready")
	start := time.Now()
	_, err := c.exit()
	require.Less(t, time.Since(start), 30*time.Second)
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.ErrorIs(t, c.ctx.Err(), context.DeadlineExceeded)
	require.NotNil(t, c.cmd.ProcessState, "子进程已被回收")
	require.Contains(t, c.describe(), "子进程超时被杀掉")
}
