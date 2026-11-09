package revision

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
)

func openTestArchive(t *testing.T, dir string) *FileArchive {
	t.Helper()
	a, err := Open(dir)
	require.NoError(t, err, "打开归档目录应成功")
	t.Cleanup(func() { require.NoError(t, a.Close(), "关闭归档应可重复调用") })
	return a
}

func TestFileArchiveReopenPreservesOriginalBytes(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	a := openTestArchive(t, dir)
	var reader Archive = a
	def, files := revisionFixture()
	source := filepath.Join(t.TempDir(), "runbook.md")
	key := "skills/oncall/resources/runbook.md"
	require.NoError(t, os.WriteFile(source, files[key], 0o600))
	var err error
	files[key], err = os.ReadFile(source)
	require.NoError(t, err)
	want, err := Seal(def, files)
	require.NoError(t, err)
	stored, err := a.Put(ctx, def, files)
	require.NoError(t, err)
	require.Equal(t, want, stored)
	_, err = reader.Load(ctx, stored.ID)
	require.NoError(t, err, "归档应实现只读接口")

	require.NoError(t, os.WriteFile(source, []byte("原文件已改为新内容"), 0o600))
	files[key][0] = 0
	stored.Files[key][0] = 1
	def.Prompts["system"] = "原始定义已修改"
	require.NoError(t, a.Close())
	reopened := openTestArchive(t, dir)
	loaded, err := reopened.Load(ctx, want.ID)
	require.NoError(t, err)
	require.Equal(t, want, loaded, "重开后应恢复全部旧定义及原始资源字节")
	require.NoError(t, Verify(loaded))
	loaded.Files[key][0] = 2
	loaded.Definition.Config[0] = '!'
	loaded.Definition.Prompts["system"] = "本次读取结果已修改"
	again, err := reopened.Load(ctx, want.ID)
	require.NoError(t, err)
	require.Equal(t, want, again, "每次读取结果必须独立拥有可变内存")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "归档完成后只能留下内容寻址对象")
	require.Equal(t, want.ID+".json", entries[0].Name())
	data, err := os.ReadFile(filepath.Join(dir, want.ID+".json"))
	require.NoError(t, err)
	var persisted AgentRevision
	require.NoError(t, json.Unmarshal(data, &persisted), "归档文件应是单一 JSON 对象")
	require.Equal(t, want, persisted)
}

func TestFileArchiveConcurrentIdempotentPut(t *testing.T) {
	dir := t.TempDir()
	archives := []*FileArchive{openTestArchive(t, dir), openTestArchive(t, dir), openTestArchive(t, dir)}
	def, files := revisionFixture()
	want := sealFixture(t)
	const workers = 18
	results := make(chan AgentRevision, workers)
	errs := make(chan error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() {
			<-start
			a := archives[i%len(archives)]
			r, err := a.Put(context.Background(), def, files)
			if err == nil {
				var loaded AgentRevision
				loaded, err = a.Load(context.Background(), r.ID)
				if err == nil {
					r = loaded
				}
			}
			results <- r
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err, "并发创建与读取不得观察到半写入对象")
	}
	for r := range results {
		require.Equal(t, want, r, "同内容跨实例并发写入须幂等")
	}
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "幂等写入不应残留临时文件")
	require.Equal(t, want.ID+".json", entries[0].Name())
}

func TestFileArchiveReopenPreservesExecutableArtifact(t *testing.T) {
	def, files := runtimeFixture("executable")
	artifact := def.Runtime.Artifact
	source := filepath.Join(t.TempDir(), "check.bin")
	wantBytes := append([]byte(nil), files[artifact]...)
	require.NoError(t, os.WriteFile(source, wantBytes, 0o700))
	var err error
	files[artifact], err = os.ReadFile(source)
	require.NoError(t, err)
	dir := t.TempDir()
	a := openTestArchive(t, dir)
	stored, err := a.Put(context.Background(), def, files)
	require.NoError(t, err, "归档必须保存摘要对应的实际二进制字节")
	require.NoError(t, os.WriteFile(source, []byte{255, 0, 128, 3, 4}, 0o700))
	files[artifact][0]++
	stored.Files[artifact][1]++
	require.NoError(t, a.Close())
	reopened := openTestArchive(t, dir)
	loaded, err := reopened.Load(context.Background(), stored.ID)
	require.NoError(t, err)
	require.Equal(t, def.Runtime, loaded.Definition.Runtime)
	require.Equal(t, wantBytes, loaded.Files[artifact], "源文件和返回值变化不得影响重开后的二进制")
	require.Equal(t, loaded.Definition.Runtime.Digest, fmt.Sprintf("%x", sha256.Sum256(loaded.Files[artifact])), "重开后的二进制必须仍匹配运行时摘要")
	require.NoError(t, Verify(loaded))
	loaded.Files[artifact][0]++
	require.ErrorIs(t, Verify(loaded), fs.ErrInvalid, "修改已加载的二进制后必须验证失败")
}

func TestFileArchiveLongJSONKeyWithArray(t *testing.T) {
	def, files := revisionFixture()
	key := strings.Repeat("k", 256<<10)
	values := make([]int, 1024)
	for i := range values {
		values[i] = i
	}
	var err error
	def.Config, err = json.Marshal(map[string][]int{key: values})
	require.NoError(t, err)
	a := openTestArchive(t, t.TempDir())
	// 长键下逐项读取数组时，不应为每个元素重复构造包含长键的 JSON 指针。
	stored, err := a.Put(context.Background(), def, files)
	require.NoError(t, err, "长键和多元素数组应可正常归档")
	loaded, err := a.Load(context.Background(), stored.ID)
	require.NoError(t, err, "长键下的数组应可正常加载")
	require.Equal(t, stored, loaded, "归档往返不得改变长键或数组内容")
	var decoded map[string][]int
	require.NoError(t, json.Unmarshal(loaded.Definition.Config, &decoded))
	require.Equal(t, map[string][]int{key: values}, decoded, "全部数组元素须完整保留")
}

func TestFileArchiveRejectsCorruptionWithoutOverwrite(t *testing.T) {
	for name, corrupt := range map[string]func([]byte) []byte{
		"追加空白":    func(data []byte) []byte { return append(data, '\n') },
		"非规范配置空白": func(data []byte) []byte { return []byte(strings.Replace(string(data), `"config":{`, `"config": {`, 1)) },
		"缺失定义字段":  func(data []byte) []byte { return []byte(strings.Replace(string(data), `"version":"v1",`, "", 1)) },
		"截断JSON":  func(_ []byte) []byte { return []byte(`{"id":`) },
		"修改定义":    func(data []byte) []byte { return []byte(strings.Replace(string(data), "policy-a", "policy-b", 1)) },
		"重复字段":    func(data []byte) []byte { return append([]byte(`{"id":"伪造",`), data[1:]...) },
		"未知字段":    func(data []byte) []byte { return append([]byte(`{"unexpected":true,`), data[1:]...) },
		"大小写字段": func(data []byte) []byte {
			return []byte(strings.Replace(string(data), `"definition":`, `"Definition":`, 1))
		},
		"尾随对象":   func(data []byte) []byte { return append(data, []byte(` {}`)...) },
		"错误文件类型": func(_ []byte) []byte { return []byte(`{"id":"invalid","definition":{},"files":[]}`) },
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			a := openTestArchive(t, dir)
			def, files := revisionFixture()
			r, err := a.Put(context.Background(), def, files)
			require.NoError(t, err)
			path := filepath.Join(dir, r.ID+".json")
			original, err := os.ReadFile(path)
			require.NoError(t, err)
			bad := corrupt(original)
			require.NotEqual(t, original, bad, "夹具必须实际修改归档内容")
			require.NoError(t, os.WriteFile(path, bad, 0o600))
			_, err = a.Load(context.Background(), r.ID)
			require.ErrorIs(t, err, fs.ErrInvalid, "损坏或不符合模式的归档应被拒绝")
			_, err = a.Put(context.Background(), def, files)
			require.ErrorIs(t, err, fs.ErrInvalid, "已有损坏对象不能当成成功去重")
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, bad, after, "写入不能覆盖已有损坏对象")
		})
	}
}

func TestFileArchiveRejectsDifferentValidRevisionAtExpectedPath(t *testing.T) {
	dir := t.TempDir()
	a := openTestArchive(t, dir)
	def, files := revisionFixture()
	first, err := a.Put(context.Background(), def, files)
	require.NoError(t, err)
	def.Policy.Version = "v2"
	second, err := a.Put(context.Background(), def, files)
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(dir, second.ID+".json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, first.ID+".json"), data, 0o600))
	_, err = a.Load(context.Background(), first.ID)
	require.ErrorIs(t, err, fs.ErrInvalid, "内容自身有效仍须与请求的寻址标识匹配")
}

func TestFileArchiveRejectsOversizedObject(t *testing.T) {
	dir := t.TempDir()
	a := openTestArchive(t, dir)
	id := strings.Repeat("a", 64)
	file, err := os.Create(filepath.Join(dir, id+".json"))
	require.NoError(t, err)
	// 稀疏文件只扩大逻辑长度，验证读取前的大小检查而不分配完整负载。
	require.NoError(t, file.Truncate(constants.MaxRevisionArchiveBytes+1))
	require.NoError(t, file.Close())
	_, err = a.Load(context.Background(), id)
	require.ErrorIs(t, err, fs.ErrInvalid, "超过归档上限应在解码前被拒绝")
}

func TestFileArchiveRejectsNonRegularObjects(t *testing.T) {
	for _, kind := range []string{"目录", "符号链接"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			a := openTestArchive(t, dir)
			def, files := revisionFixture()
			r := sealFixture(t)
			path := filepath.Join(dir, r.ID+".json")
			if kind == "目录" {
				require.NoError(t, os.Mkdir(path, 0o700))
			} else {
				data, err := json.Marshal(r)
				require.NoError(t, err)
				target := filepath.Join(dir, "target.json")
				require.NoError(t, os.WriteFile(target, data, 0o600))
				require.NoError(t, os.Symlink("target.json", path))
			}
			_, err := a.Load(context.Background(), r.ID)
			require.ErrorIs(t, err, fs.ErrInvalid, "归档只能从普通文件加载")
			_, err = a.Put(context.Background(), def, files)
			require.ErrorIs(t, err, fs.ErrInvalid, "去重时也不能接受非普通文件")
			info, err := os.Lstat(path)
			require.NoError(t, err)
			require.False(t, info.Mode().IsRegular(), "被拒绝的对象不得被覆盖")
		})
	}
}

func TestFileArchiveInvalidIDsAndMissingObject(t *testing.T) {
	a := openTestArchive(t, t.TempDir())
	for _, id := range []string{"", "../outside", "/absolute", strings.Repeat("a", 63), strings.Repeat("a", 65), strings.Repeat("A", 64), strings.Repeat("g", 64), strings.Repeat("a", 64) + ".json"} {
		_, err := a.Load(context.Background(), id)
		require.ErrorIs(t, err, fs.ErrInvalid, "非法内容标识 %q 必须被拒绝", id)
	}
	_, err := a.Load(context.Background(), strings.Repeat("0", 64))
	require.ErrorIs(t, err, fs.ErrNotExist, "合法但不存在的标识应返回不存在错误")
}

func TestFileArchiveCancellationAndClose(t *testing.T) {
	dir := t.TempDir()
	a := openTestArchive(t, dir)
	def, files := revisionFixture()
	want := sealFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.Put(ctx, def, files)
	require.ErrorIs(t, err, context.Canceled, "已取消的写入必须失败")
	_, err = a.Load(ctx, want.ID)
	require.ErrorIs(t, err, context.Canceled, "已取消的读取必须失败")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "已取消写入不得产生任何归档文件")
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	_, err = a.Put(expired, def, files)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	_, err = a.Load(expired, want.ID)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, a.Close())
	require.NoError(t, a.Close(), "关闭应幂等")
	_, err = a.Put(context.Background(), def, files)
	require.ErrorIs(t, err, fs.ErrClosed, "关闭后不得写入")
	_, err = a.Load(context.Background(), want.ID)
	require.ErrorIs(t, err, fs.ErrClosed, "关闭后不得读取")
}

func TestFileArchiveConcurrentClose(t *testing.T) {
	a := openTestArchive(t, t.TempDir())
	def, files := revisionFixture()
	r, err := a.Put(context.Background(), def, files)
	require.NoError(t, err)
	const workers = 12
	errs := make(chan error, workers*2+1)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			<-start
			_, err := a.Load(context.Background(), r.ID)
			errs <- err
			_, err = a.Put(context.Background(), def, files)
			errs <- err
		})
	}
	wg.Go(func() { <-start; errs <- a.Close() })
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			require.True(t, errors.Is(err, fs.ErrClosed), "并发关闭只允许成功或关闭错误，实际为 %v", err)
		}
	}
	_, err = a.Load(context.Background(), r.ID)
	require.ErrorIs(t, err, fs.ErrClosed)
}

func TestFileArchiveCanceledRequestsDoNotWaitForLock(t *testing.T) {
	for _, operation := range []string{"读取", "写入"} {
		t.Run(operation, func(t *testing.T) {
			a := openTestArchive(t, t.TempDir())
			def, files := revisionFixture()
			id := sealFixture(t).ID
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			done := make(chan error, 1)
			// 用独占锁确定性模拟 Close 正等待或持有锁，无需依赖调度先后。
			a.mu.Lock()
			go func() {
				var err error
				if operation == "读取" {
					_, err = a.Load(ctx, id)
				} else {
					_, err = a.Put(ctx, def, files)
				}
				done <- err
			}()
			timer := time.NewTimer(time.Second)
			defer timer.Stop()
			select {
			case err := <-done:
				a.mu.Unlock()
				require.ErrorIs(t, err, context.Canceled, "取消请求必须在获取锁之前返回")
			case <-timer.C:
				// 失败路径也先释放锁并回收请求，避免清理阶段 Close 死锁。
				a.mu.Unlock()
				<-done
				t.Fatal("已取消的请求仍在等待归档锁")
			}
		})
	}
}

func TestFileArchiveRetainsOpenedDirectory(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "archive")
	require.NoError(t, os.Mkdir(dir, 0o700))
	a := openTestArchive(t, dir)
	moved := filepath.Join(parent, "original")
	require.NoError(t, os.Rename(dir, moved))
	require.NoError(t, os.Mkdir(dir, 0o700))
	def, files := revisionFixture()
	r, err := a.Put(context.Background(), def, files)
	require.NoError(t, err, "写入须使用已持有的根目录句柄")
	_, err = os.Stat(filepath.Join(moved, r.ID+".json"))
	require.NoError(t, err, "归档应写入最初打开的目录")
	_, err = os.Stat(filepath.Join(dir, r.ID+".json"))
	require.ErrorIs(t, err, fs.ErrNotExist, "替换目录不能接收到后续写入")
	loaded, err := a.Load(context.Background(), r.ID)
	require.NoError(t, err)
	require.Equal(t, r, loaded)
}
