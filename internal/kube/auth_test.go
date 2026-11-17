package kube

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestOIDCReloadUsesRenewedKubeconfigCredentials(t *testing.T) {
	headers := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Get("Authorization")
		w.Write([]byte("{}"))
	}))
	defer server.Close()
	token := func(subject string) string {
		return base64.RawURLEncoding.EncodeToString([]byte("{}")) + "." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("{\"sub\":%q,\"exp\":%d}", subject, time.Now().Add(time.Hour).Unix()))) + ".signature"
	}
	first, second := token("first"), token("renewed")
	for _, value := range []string{first, second} {
		cfg := &rest.Config{Host: server.URL, AuthProvider: &clientcmdapi.AuthProviderConfig{Name: "oidc", Config: map[string]string{"idp-issuer-url": server.URL, "client-id": "test", "id-token": value, "refresh-token": "unused"}}}
		if err := freshOIDCConfig(cfg); err != nil {
			t.Fatal(err)
		}
		client, err := rest.HTTPClientFor(cfg)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if got := <-headers; got != "Bearer "+value {
			t.Fatal("reloaded configuration reused stale OIDC credentials")
		}
	}
}

func TestLiveKubeAuthProbe(t *testing.T) {
	file := os.Getenv("FARSHMAC_KUBE_AUTH_PROBE")
	if file == "" {
		t.Skip("opt-in read-only authentication probe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := Connect(ctx, Profile{Kubeconfig: file, Context: os.Getenv("FARSHMAC_KUBE_AUTH_CONTEXT")})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.ServerVersion() == "" {
		t.Fatal("empty version")
	}
	t.Log("Authenticated read-only API request succeeded")
}
