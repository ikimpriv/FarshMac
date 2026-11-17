package kube

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"k8s.io/client-go/rest"
)

// Legacy client-go OIDC providers are globally cached without the credential
// generation. A GUI must not reuse tokens predating an external kubectl login.
// The discriminator affects only the provider cache, never the HTTP endpoint.
func freshOIDCConfig(cfg *rest.Config) error {
	if cfg.AuthProvider == nil || cfg.AuthProvider.Name != "oidc" {
		return nil
	}
	encoded, _ := json.Marshal(cfg.AuthProvider.Config)
	digest := sha256.Sum256(encoded)
	provider, err := rest.GetAuthProvider(cfg.Host+"#farshmac-"+hex.EncodeToString(digest[:]), cfg.AuthProvider, cfg.AuthConfigPersister)
	if err != nil {
		return err
	}
	cfg.AuthProvider = nil
	cfg.Wrap(provider.WrapTransport)
	return nil
}
