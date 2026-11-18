package docker

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestToolboxMatchesTargetAppArmor(t *testing.T) {
	for _, profile := range []string{"", "docker-default", "unconfined", "custom-target"} {
		args := toolboxArgs("target", "image", "old", profile)
		joined := strings.Join(args, " ")
		if slices.Contains(args, "--privileged") || !strings.Contains(joined, "--user 0") || !strings.Contains(joined, "io.farshmac.podtoast.replaces=old") {
			t.Fatal(args)
		}
		want := profile != "" && profile != "docker-default"
		if slices.Contains(args, "--security-opt") != want {
			t.Fatal(args)
		}
		if want && !slices.Contains(args, "apparmor="+profile) {
			t.Fatal(args)
		}
	}
}
func TestToolboxRemoteTargetAccess(t *testing.T) {
	target := os.Getenv("FARSHMAC_TOOLBOX_TEST_TARGET")
	if target == "" {
		t.Skip("opt-in disposable remote toolbox test")
	}
	client := Client{Context: os.Getenv("FARSHMAC_TOOLBOX_TEST_CONTEXT")}
	image := os.Getenv("FARSHMAC_TOOLBOX_TEST_IMAGE")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	helper, err := client.EnsureToolbox(ctx, target, image, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		if _, e := client.Run(cleanup, "rm", "--force", helper); e != nil {
			t.Error(e)
		}
	}()
	agent, err := client.Agent(ctx, helper, target)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	fs := agent.FS(ctx)
	defer fs.Close()
	if _, err = fs.Stat("/"); err != nil {
		t.Fatal(err)
	}
	if _, err = fs.ReadDir("/"); err != nil {
		t.Fatal(err)
	}
	reused, err := client.EnsureToolbox(ctx, target, image, helper)
	if err != nil || reused != helper {
		t.Fatal("healthy toolbox not reused", reused, err)
	}
}
