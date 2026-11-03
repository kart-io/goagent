package env

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// LocalExecutor 在宿主机工作区目录中直接执行命令，不做文件系统与网络隔离。
// Unix 上命令在独立进程组中运行，命令结束或超时后结束整个进程组；用 setsid 等方式脱离进程组的子进程不在此列。
type LocalExecutor struct {
	// Root 是工作区根目录，与 FileSystem 的根目录一致。
	Root string
}

// Capabilities 实现 Executor。
func (LocalExecutor) Capabilities() Capabilities { return Capabilities{Network: true} }

// Run 实现 Executor。
func (e LocalExecutor) Run(ctx context.Context, cmd Command) (Result, error) {
	if len(cmd.Argv) == 0 {
		return Result{}, errors.New("empty command")
	}
	c := exec.CommandContext(ctx, cmd.Argv[0], cmd.Argv[1:]...)
	c.Dir = filepath.Join(e.Root, filepath.FromSlash(cmd.Dir))
	// 后台子进程可能一直持有输出管道，命令退出或超时后最多再等 1 秒。
	c.WaitDelay = time.Second
	isolateProcessGroup(c)
	res, err := runExec(ctx, c)
	_ = killProcessGroup(c)
	return res, err
}

// DockerExecutor 在一次性容器中执行命令：工作区挂载到 /workspace，默认断网，
// 在宿主机上以当前用户的 uid:gid 运行以免生成 root 所有的文件，HOME 设为 /tmp。
type DockerExecutor struct {
	// Root 是挂载进容器的宿主机工作区目录。
	Root string
	// Image 是容器镜像，需包含要执行的命令（如 git、go）。
	Image string
	// Network 是 docker --network 的取值，空串表示 "none"。
	Network string
	// ReadOnly 以只读方式挂载工作区。
	ReadOnly bool
	// Docker 是 docker 可执行文件，空串表示 "docker"。
	Docker string
}

// Capabilities 实现 Executor。
func (e DockerExecutor) Capabilities() Capabilities {
	return Capabilities{Container: true, Network: e.Network != "" && e.Network != "none", ReadOnlyRoot: e.ReadOnly}
}

// Run 实现 Executor。先创建再启动容器，结束时强制删除，ctx 何时结束都不会留下容器。
func (e DockerExecutor) Run(ctx context.Context, cmd Command) (Result, error) {
	if len(cmd.Argv) == 0 {
		return Result{}, errors.New("empty command")
	}
	if e.Image == "" {
		return Result{}, errors.New("docker executor image is required")
	}
	docker := e.Docker
	if docker == "" {
		docker = "docker"
	}
	network := e.Network
	if network == "" {
		network = "none"
	}
	name, err := containerName()
	if err != nil {
		return Result{}, err
	}
	mount := e.Root + ":/workspace"
	if e.ReadOnly {
		mount += ":ro"
	}
	args := []string{"--rm", "--name", name, "--network", network,
		"-v", mount, "-w", path.Join("/workspace", cmd.Dir),
		// 以宿主机 uid 运行时镜像里没有对应用户，HOME 默认为不可写的 /，工具链缓存会写失败。
		"-e", "HOME=/tmp"}
	if uid, gid := os.Getuid(), os.Getgid(); uid >= 0 && gid >= 0 {
		args = append(args, "--user", strconv.Itoa(uid)+":"+strconv.Itoa(gid))
	}
	args = append(args, e.Image)
	args = append(args, cmd.Argv...)

	defer func() {
		rmCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = exec.CommandContext(rmCtx, docker, "rm", "-f", name).Run()
	}()
	if res, err := createContainer(ctx, docker, e.Image, args); err != nil || res.ExitCode != 0 {
		return res, err
	}
	c := exec.CommandContext(ctx, docker, "start", "-a", name)
	c.WaitDelay = time.Second
	res, err := runExec(ctx, c)
	if err == nil && res.ExitCode != 0 {
		if code, ok := startFailureCode(res.Output); ok {
			res.ExitCode = code
		}
	}
	return res, err
}

// createTimeout 限制不随 ctx 取消的 docker create。镜像已在本地，创建通常一两秒内完成。
const createTimeout = time.Minute

// createContainer 创建容器但不启动。docker run 被中断时守护进程可能仍在创建容器，之后的 rm 删不到它，
// 所以创建不随 ctx 取消，等容器确实存在后再由调用方删除。镜像不在本地时先拉取，拉取可以随 ctx 取消。
func createContainer(ctx context.Context, docker, image string, args []string) (Result, error) {
	for pulled := false; ; pulled = true {
		createCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), createTimeout)
		c := exec.CommandContext(createCtx, docker, append([]string{"create", "--pull", "never"}, args...)...)
		c.WaitDelay = time.Second
		res, err := runExec(createCtx, c)
		cancel()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return res, ctxErr
		}
		if err != nil || res.ExitCode == 0 || pulled {
			return res, err
		}
		inspect := exec.CommandContext(ctx, docker, "image", "inspect", image)
		inspect.WaitDelay = time.Second
		if inspect.Run() == nil {
			return res, nil
		}
		pull := exec.CommandContext(ctx, docker, "pull", "-q", image)
		pull.WaitDelay = time.Second
		pullRes, pullErr := runExec(ctx, pull)
		if pullErr != nil || pullRes.ExitCode != 0 {
			pullRes.Output = append(res.Output, pullRes.Output...)
			return pullRes, pullErr
		}
	}
}

// startFailureCode 还原容器没能启动时的退出码。docker start 此时一律返回 1，
// docker run 则按同样的报错返回 127（找不到命令）或 126（不可执行）。
func startFailureCode(output []byte) (int, bool) {
	text := string(output)
	if !strings.Contains(text, "Error response from daemon: failed to create task for container") {
		return 0, false
	}
	switch {
	case strings.Contains(text, "executable file not found"), strings.Contains(text, "no such file or directory"):
		return 127, true
	case strings.Contains(text, "permission denied"):
		return 126, true
	}
	return 125, true
}

func containerName() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "goagent-env-" + hex.EncodeToString(b), nil
}

func runExec(ctx context.Context, c *exec.Cmd) (Result, error) {
	out, err := c.CombinedOutput()
	res := Result{Output: out}
	if err == nil {
		return res, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return res, ctxErr
	}
	// 命令已退出，只是留在后台的子进程仍持有输出管道。
	if errors.Is(err, exec.ErrWaitDelay) && c.ProcessState != nil {
		res.ExitCode = c.ProcessState.ExitCode()
		return res, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	}
	return res, err
}
