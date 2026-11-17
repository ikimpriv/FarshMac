package docker

import (
	"bytes"
	"context"
	"farshmac/internal/podtoast"
	"fmt"
	"strings"
)

// Agent speaks PodToast HTTP/2 through docker exec. It opens no listening port.
func (c Client) Agent(ctx context.Context, helper, target string) (*podtoast.Client, error) {
	return c.AgentCommand(ctx, helper, "serve", "--target-id", target)
}
func (c Client) AgentCommand(ctx context.Context, helper string, args ...string) (*podtoast.Client, error) {
	lifetime, cancel := context.WithCancel(context.Background())
	argv := append([]string{"exec", "-i", helper, "/podtoast"}, args...)
	cmd := c.command(lifetime, argv...)
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		cancel()
		return nil, err
	}
	var diagnostics bytes.Buffer
	cmd.Stderr = &limitedBuffer{&diagnostics, 16 << 10}
	if err = cmd.Start(); err != nil {
		in.Close()
		out.Close()
		cancel()
		return nil, err
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	conn := &podtoast.StreamConn{Reader: out, Writer: in, CloseFunc: func() error { in.Close(); out.Close(); cancel(); <-done; return nil }}
	agent, err := podtoast.Connect(ctx, conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("PodToast Docker exec: %w: %s", err, strings.TrimSpace(diagnostics.String()))
	}
	return agent, nil
}
