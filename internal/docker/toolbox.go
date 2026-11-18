package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// EnsureToolbox verifies target-root access before returning a helper. Profiles
// are container-scoped: the target and daemon security settings never change.
func (c Client) EnsureToolbox(ctx context.Context, target, image, existing string) (string, error) {
	if existing != "" {
		agent, e := c.Agent(ctx, existing, target)
		if e == nil {
			agent.Close()
			return existing, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
	b, e := c.Run(ctx, "inspect", "--format", "{{json .AppArmorProfile}}", target)
	if e != nil {
		return "", e
	}
	var profile string
	if e = json.Unmarshal(b, &profile); e != nil {
		return "", fmt.Errorf("target AppArmor profile: %w", e)
	}
	args := toolboxArgs(target, image, existing, profile)
	b, e = c.Run(ctx, args...)
	if e != nil {
		return "", e
	}
	id := strings.TrimSpace(string(b))
	if id == "" {
		return "", fmt.Errorf("Docker returned an empty toolbox ID")
	}
	agent, e := c.Agent(ctx, id, target)
	if e != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, removeErr := c.Run(cleanup, "rm", "--force", id)
		detail := ""
		if removeErr != nil {
			detail = "; failed toolbox " + id + " could not be removed: " + removeErr.Error()
		}
		return "", fmt.Errorf("PodToast cannot access target filesystem (target AppArmor %q): %w%s", profile, e, detail)
	}
	agent.Close()
	return id, nil
}
func toolboxArgs(target, image, existing, profile string) []string {
	args := []string{"run", "-d", "--user", "0", "--label", "io.farshmac.podtoast.target=" + target, "--pid=container:" + target, "--network=container:" + target, "--cap-add=SYS_PTRACE"}
	if existing != "" {
		args = append(args, "--label", "io.farshmac.podtoast.replaces="+existing)
	}
	// docker-default permits ptrace only to its own peer profile. Matching the
	// target allows proc-root access without --privileged or host namespaces.
	if profile != "" && profile != "docker-default" {
		args = append(args, "--security-opt", "apparmor="+profile)
	}
	return append(args, image, "idle")
}
