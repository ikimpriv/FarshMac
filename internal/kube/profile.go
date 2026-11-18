// Package kube shows a Kubernetes cluster as a file system for the panel:
// namespaces, resource type folders, objects, owned children, pod
// containers and container file systems. See docs/KUBERNETES_PANEL_SPEC.md.
package kube

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"k8s.io/client-go/tools/clientcmd"
)

// Profile is a saved cluster connection: a kubeconfig file and the context
// in it. It carries no secrets; credentials stay in the kubeconfig.
type Profile struct {
	Name                                                 string
	Kubeconfig                                           string
	Context                                              string
	SSHHost, SSHUser, SSHPort, SSHKeyFile, SSHConfigFile string
	SSHTrustFile                                         string // Explicitly confirmed host keys, never an insecure bypass.
	SSHAuth                                              string // automatic, key or password.
	SSHSecret                                            string `json:"-"` // Password/passphrase retained only for this app session.
	StoredConfig                                         bool   // Managed, named pasted kubeconfig.
	RemoteBase                                           string // Original remote config path for relative credential references.
	RemoteConfig                                         bool   // Kubeconfig and referenced credential files are read on the SSH server.
	ExtraPath                                            string // Folders appended to PATH for exec auth plugins (C13).
}

// Label names the connection the way the panel shows it.
func (p Profile) Label() string {
	label := "k8s:" + p.Context
	if p.SSHHost != "" {
		label += " via " + p.SSHHost
	}
	return label
}

// Identity distinguishes equal context names on different SSH hosts/config sources.
func (p Profile) Identity() string {
	base := p.Kubeconfig + "\x00" + p.Context
	if p.SSHHost == "" && !p.RemoteConfig {
		return base
	}
	return base + "\x00" + strings.Join([]string{p.SSHHost, p.SSHUser, p.SSHPort, p.SSHKeyFile, p.SSHConfigFile, fmt.Sprint(p.RemoteConfig)}, "\x00")
}

// DefaultKubeconfig is the first path of $KUBECONFIG, else ~/.kube/config.
func DefaultKubeconfig() string {
	if env := os.Getenv("KUBECONFIG"); env != "" {
		for _, part := range filepath.SplitList(env) {
			if part != "" {
				return part
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kube", "config")
}

// Contexts lists the context names of a kubeconfig file, sorted, with the
// file's current context.
func Contexts(kubeconfig string) (names []string, current string, err error) {
	cfg, err := clientcmd.LoadFromFile(kubeconfig)
	if err != nil {
		return nil, "", err
	}
	for name := range cfg.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, cfg.CurrentContext, nil
}

var loginPath struct {
	once sync.Once
	path string
}

// EnsureLoginPath makes the PATH of the user's login shell visible to this
// process, so exec auth plugins (gke-gcloud-auth-plugin, aws, kubelogin) are
// found when the app was started from Finder with a minimal PATH. The shell
// is asked once per process; extra folders are added on every call.
func EnsureLoginPath(extra string) {
	loginPath.once.Do(func() { loginPath.path = shellPath() })
	merged := joinPaths(extra, loginPath.path, os.Getenv("PATH"))
	if merged != "" && merged != os.Getenv("PATH") {
		os.Setenv("PATH", merged)
	}
}

func shellPath() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, shell, "-lc", `printf %s "$PATH"`).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// joinPaths concatenates PATH lists, dropping empty and repeated folders.
func joinPaths(lists ...string) string {
	seen := map[string]bool{}
	var parts []string
	for _, list := range lists {
		for _, dir := range filepath.SplitList(list) {
			if dir == "" || seen[dir] {
				continue
			}
			seen[dir] = true
			parts = append(parts, dir)
		}
	}
	return strings.Join(parts, string(os.PathListSeparator))
}
