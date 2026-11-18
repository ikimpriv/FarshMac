// Package docker integrates Docker Engine through its installed CLI, retaining
// the user's selected context and credential helpers without exposing sockets.
package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"farshmac/internal/kube"
	"github.com/creack/pty"
	utilexec "k8s.io/client-go/util/exec"
)

type Client struct {
	Context string
	Binary  string
}
type Container struct{ ID, Names, State, Status, Image, Labels, CreatedAt string }

func (c Container) Label(key string) string {
	for _, s := range strings.Split(c.Labels, ",") {
		k, v, ok := strings.Cut(s, "=")
		if ok && k == key {
			return v
		}
	}
	return ""
}
func (c Client) command(ctx context.Context, args ...string) *exec.Cmd {
	binary := c.Binary
	if binary == "" {
		binary = "docker"
	}
	if c.Context != "" {
		args = append([]string{"--context", c.Context}, args...)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.WaitDelay = 2 * time.Second
	return cmd
}
func (c Client) Run(ctx context.Context, args ...string) ([]byte, error) {
	var out, diagnostic bytes.Buffer
	cmd := c.command(ctx, args...)
	cmd.Stdout = &limitedBuffer{&out, 16 << 20}
	cmd.Stderr = &limitedBuffer{&diagnostic, 16 << 10}
	if len(args) > 0 && args[0] == "logs" {
		cmd.Stderr = cmd.Stdout
	}
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("docker: %w: %s", err, strings.TrimSpace(diagnostic.String()))
	}
	return out.Bytes(), nil
}

type limitedBuffer struct {
	b     *bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.b.Len()+n > b.limit {
		return 0, fmt.Errorf("Docker response exceeds %d bytes", b.limit)
	}
	return b.b.Write(p)
}
func (c Client) Containers(ctx context.Context) ([]Container, error) {
	data, err := c.Run(ctx, "ps", "--all", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	var entries []Container
	decoder := json.NewDecoder(bytes.NewReader(data))
	for decoder.More() {
		var entry Container
		if err := decoder.Decode(&entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
func (c Client) Names(ctx context.Context, kind string) ([]string, error) {
	if kind != "volume" && kind != "network" && kind != "context" {
		return nil, fmt.Errorf("unsupported Docker catalog")
	}
	field := "{{.Name}}"
	b, err := c.Run(ctx, kind, "ls", "--format", field)
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(b)), nil
}
func (c Client) Stream(ctx context.Context, ref kube.ContainerRef, o kube.ExecOptions) error {
	args := []string{"exec", "-i"}
	if o.TTY {
		args = append(args, "-t", "-e", "TERM=xterm-256color")
	}
	args = append(args, ref.Container)
	args = append(args, o.Command...)
	cmd := c.command(ctx, args...)
	if !o.TTY {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = o.Stdin, o.Stdout, o.Stderr
		return exitError(cmd.Run())
	}
	size := &pty.Winsize{Cols: 80, Rows: 24}
	if o.Resize != nil {
		select {
		case s := <-o.Resize:
			size = &pty.Winsize{Cols: s.Width, Rows: s.Height}
		default:
		}
	}
	master, err := pty.StartWithSize(cmd, size)
	if err != nil {
		return err
	}
	defer master.Close()
	if o.Stdin != nil {
		go io.Copy(master, o.Stdin)
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case size, ok := <-o.Resize:
				if !ok {
					return
				}
				_ = pty.Setsize(master, &pty.Winsize{Cols: size.Width, Rows: size.Height})
			case <-stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	copied := make(chan struct{})
	go func() {
		defer close(copied)
		dst := o.Stdout
		if dst == nil {
			dst = io.Discard
		}
		io.Copy(dst, master)
	}()
	err = cmd.Wait()
	<-copied
	master.Close()
	return exitError(err)
}
func exitError(err error) error {
	var e *exec.ExitError
	if errors.As(err, &e) {
		return utilexec.CodeExitError{Err: err, Code: e.ExitCode()}
	}
	return err
}

// RunTo streams large exports without retaining the archive in memory.
func (c Client) RunTo(ctx context.Context, out io.Writer, args ...string) error {
	var diagnostic bytes.Buffer
	cmd := c.command(ctx, args...)
	cmd.Stdout = out
	cmd.Stderr = &limitedBuffer{&diagnostic, 16 << 10}
	err := cmd.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("docker: %w: %s", err, strings.TrimSpace(diagnostic.String()))
	}
	return nil
}
