package kube

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"k8s.io/client-go/tools/clientcmd"
	api "k8s.io/client-go/tools/clientcmd/api"
)

const maxKubeconfigBytes = 4 << 20

type ConfigSource struct {
	Path     string
	Contexts []string
	Current  string
}

func expandLocalHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
	}
	return p
}

func readConfigSource(ctx context.Context, p Profile, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.RemoteConfig {
		if p.SSHHost == "" {
			return nil, fmt.Errorf("remote kubeconfig requires an SSH host")
		}
		if strings.ContainsRune(name, 0) {
			return nil, fmt.Errorf("invalid remote path")
		}
		script := "p=" + shellQuote(name) + `; case "$p" in '~/'*) p="$HOME/${p#\~/}";; '~') p="$HOME";; esac; cat -- "$p"`
		return sshCommand(ctx, p, script, maxKubeconfigBytes)
	}
	f, err := os.Open(expandLocalHome(name))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular kubeconfig file")
	}
	// A regular file can grow after Stat; enforce the bound while reading too.
	return readLimitedConfig(f)
}
func sourceFromBytes(name string, b []byte) (ConfigSource, error) {
	cfg, err := clientcmd.Load(b)
	if err != nil {
		return ConfigSource{}, fmt.Errorf("cannot parse kubeconfig %s", name)
	}
	s := ConfigSource{Path: name, Current: cfg.CurrentContext}
	for name := range cfg.Contexts {
		s.Contexts = append(s.Contexts, name)
	}
	sort.Strings(s.Contexts)
	if len(s.Contexts) == 0 {
		return s, fmt.Errorf("no contexts in %s", name)
	}
	return s, nil
}
func ReadConfigSource(ctx context.Context, p Profile) (ConfigSource, error) {
	b, e := readConfigSource(ctx, p, p.Kubeconfig)
	if e != nil {
		return ConfigSource{}, e
	}
	return sourceFromBytes(p.Kubeconfig, b)
}

// Discovery is shallow and bounded: skip cache directories and never search an
// entire home directory. A manually entered path can always reach other files.
func DiscoverConfigs(ctx context.Context, p Profile) ([]ConfigSource, []string, error) {
	var candidates []string
	if p.RemoteConfig {
		script := `emit() { [ -f "$1" ] && [ -r "$1" ] && printf '%s\000' "$1"; }
oldifs=$IFS; IFS=:; for f in $KUBECONFIG; do emit "$f"; done; IFS=$oldifs
emit "$HOME/.kube/config"
for f in "$HOME"/.kube/*; do
 if [ -f "$f" ]; then emit "$f"; elif [ -d "$f" ]; then
  case "$f" in */cache|*/http-cache) continue;; esac
  for g in "$f"/*; do emit "$g"; done
 fi
done
emit /etc/rancher/k3s/k3s.yaml
emit /etc/kubernetes/admin.conf
exit 0`
		b, err := sshCommand(ctx, p, script, 64<<10)
		if err != nil {
			return nil, nil, err
		}
		candidates = strings.Split(string(b), "\x00")
	} else {
		candidates = append(candidates, filepath.SplitList(os.Getenv("KUBECONFIG"))...)
		candidates = append(candidates, DefaultKubeconfig())
		home, _ := os.UserHomeDir()
		root := filepath.Join(home, ".kube")
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			name := filepath.Join(root, e.Name())
			if e.IsDir() {
				if e.Name() == "cache" || e.Name() == "http-cache" {
					continue
				}
				nested, _ := os.ReadDir(name)
				for _, sub := range nested {
					if !sub.IsDir() {
						candidates = append(candidates, filepath.Join(name, sub.Name()))
					}
				}
			} else {
				candidates = append(candidates, name)
			}
		}
	}
	seen := map[string]bool{}
	var sources []ConfigSource
	var warnings []string
	for _, name := range candidates {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if len(seen) > 128 {
			warnings = append(warnings, "Discovery limited to 128 files; enter an explicit path to read more.")
			break
		}
		b, err := readConfigSource(ctx, p, name)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			warnings = append(warnings, "Cannot read "+name)
			continue
		}
		source, err := sourceFromBytes(name, b)
		clear(b)
		if err == nil {
			sources = append(sources, source)
		}
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	return sources, warnings, nil
}

func remoteClientConfig(ctx context.Context, p Profile) (*api.Config, error) {
	b, err := readConfigSource(ctx, p, p.Kubeconfig)
	if err != nil {
		return nil, err
	}
	defer clear(b)
	cfg, err := clientcmd.Load(b)
	if err != nil {
		return nil, fmt.Errorf("cannot parse remote kubeconfig")
	}
	if p.Context != "" {
		cfg.CurrentContext = p.Context
	}
	if err = api.MinifyConfig(cfg); err != nil {
		return nil, err
	}
	fetch := func(name string) ([]byte, error) {
		base := p.Kubeconfig
		if p.StoredConfig {
			base = p.RemoteBase
		}
		if !path.IsAbs(name) && !strings.HasPrefix(name, "~/") {
			if base == "" {
				return nil, fmt.Errorf("relative credential paths require the original remote kubeconfig path, or embedded certificate/key data")
			}
			name = path.Join(path.Dir(base), name)
		}
		remote := p
		remote.RemoteConfig = true
		return readConfigSource(ctx, remote, name)
	}
	for _, cluster := range cfg.Clusters {
		if cluster.ProxyURL != "" {
			return nil, fmt.Errorf("remote kubeconfig proxy-url is not supported through SSH; use an SSH host with direct API access")
		}
		if cluster.CertificateAuthority != "" && len(cluster.CertificateAuthorityData) == 0 {
			cluster.CertificateAuthorityData, err = fetch(cluster.CertificateAuthority)
			if err != nil {
				return nil, fmt.Errorf("read remote CA: %w", err)
			}
		}
		cluster.CertificateAuthority = ""
		cluster.LocationOfOrigin = ""
	}
	for _, auth := range cfg.AuthInfos {
		// Remote exec commands are not local programs. Never run them on the Mac.
		if auth.Exec != nil || auth.AuthProvider != nil && auth.AuthProvider.Name != "oidc" {
			return nil, fmt.Errorf("this remote context uses an exec credential plugin; use a local kubeconfig with that plugin installed locally, or a remote context with certificate/token credentials")
		}
		if auth.ClientCertificate != "" && len(auth.ClientCertificateData) == 0 {
			auth.ClientCertificateData, err = fetch(auth.ClientCertificate)
			if err != nil {
				return nil, fmt.Errorf("read remote client certificate: %w", err)
			}
		}
		if auth.ClientKey != "" && len(auth.ClientKeyData) == 0 {
			auth.ClientKeyData, err = fetch(auth.ClientKey)
			if err != nil {
				return nil, fmt.Errorf("read remote client key: %w", err)
			}
		}
		if auth.TokenFile != "" {
			token, e := fetch(auth.TokenFile)
			if e != nil {
				return nil, fmt.Errorf("read remote token: %w", e)
			}
			auth.Token = strings.TrimSpace(string(token))
			clear(token)
		}
		auth.ClientCertificate = ""
		auth.ClientKey = ""
		auth.TokenFile = ""
		auth.LocationOfOrigin = ""
	}
	return cfg, nil
}

func readLimitedConfig(r io.Reader) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, maxKubeconfigBytes+1))
	if e != nil {
		return nil, e
	}
	if len(b) > maxKubeconfigBytes {
		clear(b)
		return nil, fmt.Errorf("kubeconfig exceeds 4 MiB")
	}
	return b, nil
}

// InspectConfig validates pasted YAML without printing its credential contents.
func InspectConfig(data []byte) (ConfigSource, error) {
	if len(data) > maxKubeconfigBytes {
		return ConfigSource{}, fmt.Errorf("kubeconfig exceeds 4 MiB")
	}
	return sourceFromBytes("pasted config", data)
}
