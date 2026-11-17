// A shell-free application used only by the isolated PodToast integration test.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "echo" {
		_, _ = io.Copy(os.Stdout, os.Stdin)
		return
	}
	if err := os.MkdirAll("/data", 0755); err != nil {
		panic(err)
	}
	if err := os.WriteFile("/data/hello.txt", []byte("hello from distroless\n"), 0640); err != nil {
		panic(err)
	}
	_ = os.Symlink("/data/hello.txt", "/data/absolute-link")
	_ = os.Symlink("hello.txt", "/data/relative-link")
	for i := 0; i < 2000; i++ {
		_ = os.WriteFile(fmt.Sprintf("/data/file-%04d", i), []byte("fixture"), 0600)
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "PodToast fixture\n") })
	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprint(w, "# TYPE fixture_requests_total counter\nfixture_requests_total{method=\"GET\"} 42\n# TYPE fixture_memory_bytes gauge\nfixture_memory_bytes 1048576\n")
	})
	http.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Minute):
		}
	})
	address := os.Getenv("LISTEN_ADDRESS")
	if address == "" {
		address = ":8080"
	}
	if err := http.ListenAndServe(address, nil); err != nil {
		panic(err)
	}
}
