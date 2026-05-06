package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const maxOutputBytes = 10 * 1024 * 1024

type ExecOptions struct {
	Timeout time.Duration
	Env     []string
}

type ExecResult struct {
	Stdout string
	Stderr string
	Code   int
}

type Executor interface {
	Exec(ctx context.Context, channelID string, command string, opts ExecOptions) (ExecResult, error)
}

type runCmdFn func(ctx context.Context, name string, args ...string) *exec.Cmd

type DockerExecutor struct {
	container string
	dataDir   string
	runCmd    runCmdFn
}

func NewDockerExecutor(container, dataDir string) (*DockerExecutor, error) {
	return newDockerExecutor(container, dataDir, exec.CommandContext)
}

func newDockerExecutor(container, dataDir string, runCmd runCmdFn) (*DockerExecutor, error) {
	ctx := context.Background()

	if err := runCmd(ctx, "docker", "--version").Run(); err != nil {
		return nil, fmt.Errorf("docker not available: %w", err)
	}

	out, err := runCmd(ctx, "docker", "inspect", "-f", "{{.State.Running}}", container).Output()
	if err != nil {
		return nil, fmt.Errorf("container %q not found: %w", container, err)
	}
	if strings.TrimSpace(string(out)) != "true" {
		return nil, fmt.Errorf("container %q is not running", container)
	}

	if err := runCmd(ctx, "docker", "exec", container, "test", "-d", "/workspace").Run(); err != nil {
		return nil, fmt.Errorf("container %q missing /workspace mount: %w", container, err)
	}

	return &DockerExecutor{container: container, dataDir: dataDir, runCmd: runCmd}, nil
}

func (d *DockerExecutor) Exec(ctx context.Context, channelID string, command string, opts ExecOptions) (ExecResult, error) {
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	workDir := "/workspace/" + channelID
	args := []string{"exec", "-w", workDir}
	for _, e := range opts.Env {
		args = append(args, "-e", e)
	}
	args = append(args, d.container, "sh", "-c", command)

	cmd := d.runCmd(ctx, "docker", args...)

	var stdoutBuf, stderrBuf limitedBuffer
	stdoutBuf.limit = maxOutputBytes
	stderrBuf.limit = maxOutputBytes
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			return ExecResult{}, fmt.Errorf("exec: %w", err)
		}
	}

	return ExecResult{
		Stdout: stdoutBuf.String(),
		Stderr: stderrBuf.String(),
		Code:   code,
	}, nil
}

type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (lb *limitedBuffer) Write(p []byte) (int, error) {
	if lb.buf.Len() < lb.limit {
		remaining := lb.limit - lb.buf.Len()
		toWrite := p
		if len(toWrite) > remaining {
			toWrite = p[:remaining]
		}
		lb.buf.Write(toWrite)
	}
	return len(p), nil
}

func (lb *limitedBuffer) String() string {
	return lb.buf.String()
}
