package kube

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // Auth providers (gcp, oidc, azure, exec).
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

// Client is one connection to a cluster. All methods take a context and
// stop when it is cancelled. Values that come from the server are data:
// they are never interpreted as commands.
type Client struct {
	Profile Profile

	config    *rest.Config
	raw       *rest.RESTClient // Unversioned client for Table LISTs by absolute path.
	core      kubernetes.Interface
	dyn       dynamic.Interface
	disc      discovery.CachedDiscoveryInterface
	mapper    meta.RESTMapper
	version   string
	resources *resourceCache
}

// Connect opens the profile's context and verifies it by asking the server
// version. Errors keep client-go's text, which includes the stderr of a
// failed exec credential plugin.
func Connect(ctx context.Context, p Profile) (*Client, error) {
	EnsureLoginPath(p.ExtraPath)
	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: p.Kubeconfig}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: p.Context}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("kubeconfig %s: %w", p.Kubeconfig, err)
	}
	return NewFromConfig(ctx, cfg, p)
}

// NewFromConfig builds a client from a REST configuration (tests use it
// against a fake API server) and verifies the connection.
func NewFromConfig(ctx context.Context, cfg *rest.Config, p Profile) (*Client, error) {
	cfg = rest.CopyConfig(cfg)
	cfg.QPS = 50
	cfg.Burst = 300
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	cfg.UserAgent = "FarshMac"
	core, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	rawCfg := rest.CopyConfig(cfg)
	rawCfg.GroupVersion = &schema.GroupVersion{}
	rawCfg.APIPath = "/"
	rawCfg.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	raw, err := rest.UnversionedRESTClientFor(rawCfg)
	if err != nil {
		return nil, err
	}
	disc := memory.NewMemCacheClient(core.Discovery())
	c := &Client{Profile: p, config: cfg, raw: raw, core: core, dyn: dyn, disc: disc, mapper: restmapper.NewDeferredDiscoveryRESTMapper(disc)}
	version, err := core.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", cfg.Host, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.version = version.GitVersion
	return c, nil
}

// ServerVersion is the git version reported when connecting.
func (c *Client) ServerVersion() string { return c.version }

// Core is the typed client (logs, exec, namespaces).
func (c *Client) Core() kubernetes.Interface { return c.core }

// Dynamic is the client for any resource, CRDs included.
func (c *Client) Dynamic() dynamic.Interface { return c.dyn }

// Config is the REST configuration the client was built with.
func (c *Client) Config() *rest.Config { return rest.CopyConfig(c.config) }

// Close forgets cached discovery. Idle HTTP connections belong to
// client-go's shared transport cache and close on their own.
func (c *Client) Close() {
	discoveryMu.Lock()
	c.resources = nil
	discoveryMu.Unlock()
	if c.disc != nil {
		c.disc.Invalidate()
	}
}
