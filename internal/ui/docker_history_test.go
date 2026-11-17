package ui

import (
	"context"
	"farshmac/internal/docker"
	"farshmac/internal/podtoast"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDockerAutonomousMetricsHistory(t *testing.T) {
	image := os.Getenv("FARSHMAC_DOCKER_TEST_IMAGE")
	if image == "" {
		t.Skip("opt-in new PodToast image test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client := docker.Client{}
	run := func(args ...string) string {
		t.Helper()
		b, e := client.Run(ctx, args...)
		if e != nil {
			t.Fatal(e)
		}
		return strings.TrimSpace(string(b))
	}
	target := run("run", "-d", "--network=none", "--entrypoint=/bin/sh", image, "-c", "sleep 120")
	defer client.Run(context.Background(), "rm", "-f", target)
	helper := run("run", "-d", "--pid=container:"+target, "--network=container:"+target, "--cap-add=SYS_PTRACE", image, "idle")
	defer client.Run(context.Background(), "rm", "-f", helper)
	// No client, HTTP request or exec session exists during this period.
	time.Sleep(11 * time.Second)
	agent, err := client.Agent(ctx, helper, target)
	if err != nil {
		t.Fatal(err)
	}
	h, err := agent.MetricsHistory(ctx)
	agent.Close()
	if err != nil || len(h.Frames) < 2 {
		t.Fatal("idle agent did not collect history", len(h.Frames), err)
	}
	first := h.Frames[0].At
	run("exec", target, "/bin/sh", "-c", "mkdir -p /tmp/summary-fixture/sub; printf abc > /tmp/summary-fixture/a; printf defg > /tmp/summary-fixture/sub/b")
	agent, err = client.Agent(ctx, helper, target)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	frames := 0
	summary, err := agent.FS(ctx).DirectorySummary("/tmp/summary-fixture", false, func(s podtoast.DirectorySummary) error { frames++; return nil })
	if err != nil || !summary.Done || summary.Files != 2 || summary.Directories != 1 || summary.Bytes != 7 || frames < 2 {
		t.Fatalf("summary %+v frames=%d err=%v", summary, frames, err)
	}
	h, err = agent.MetricsHistory(ctx)
	if err != nil || len(h.Frames) < 2 || h.Frames[0].At != first {
		t.Fatal("history lost across exec reconnect", err)
	}
}
