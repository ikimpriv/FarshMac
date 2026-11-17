package kube

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	livemetrics "farshmac/internal/metrics"
	"farshmac/internal/podtoast"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Only an explicit kubeconfig for a disposable test cluster enables this test.
// Never reads the default kubeconfig or modifies a user's active cluster.
func TestPodToastDistroless(t *testing.T) {
	config := os.Getenv("PODTOAST_TEST_KUBECONFIG")
	if config == "" {
		t.Skip("isolated cluster not requested")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	c, err := Connect(ctx, Profile{Kubeconfig: config})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ns := "podtoast-e2e"
	_, err = c.Core().CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Core().CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{})
	uid := int64(65532)
	readonly := true
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "distroless", Namespace: ns, Annotations: map[string]string{"prometheus.io/scrape": "true", "prometheus.io/port": "8080"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "podtoast-fixture:dev", Ports: []corev1.ContainerPort{{Name: "metrics", ContainerPort: 8080}}, ImagePullPolicy: corev1.PullNever, SecurityContext: &corev1.SecurityContext{RunAsUser: &uid, ReadOnlyRootFilesystem: &readonly}, VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/data"}}}}, Volumes: []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}}}
	_, err = c.Core().CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for {
		pod, err = c.Pod(ctx, ns, pod.Name)
		if err != nil {
			t.Fatal(err)
		}
		if len(pod.Status.ContainerStatuses) > 0 && pod.Status.ContainerStatuses[0].State.Running != nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
	image := os.Getenv("PODTOAST_TEST_IMAGE")
	if image == "" {
		image = "podtoast:dev"
	}
	ref := ContainerRef{Namespace: ns, Pod: pod.Name, Container: "app"}
	client, err := c.StartPodToast(ctx, ref, image)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	t.Run("reuse existing agent after reconnect", func(t *testing.T) {
		reused, e := c.ConnectExistingPodToast(ctx, ref)
		if e != nil {
			t.Fatal(e)
		}
		defer reused.Close()
		if _, e = reused.FS(ctx).Lstat("/data/hello.txt"); e != nil {
			t.Fatal(e)
		}
		again, e := c.StartPodToast(ctx, ref, "unused.invalid/podtoast:never-pull")
		if e != nil {
			t.Fatal(e)
		}
		defer again.Close()
		current, e := c.Pod(ctx, ns, ref.Pod)
		if e != nil {
			t.Fatal(e)
		}
		if len(current.Spec.EphemeralContainers) != 1 {
			t.Fatal("existing agent was duplicated")
		}
	})

	t.Run("toolbox command in target cwd", func(t *testing.T) {
		d, e := c.ResolveDebugShell(ctx, ref, "")
		if e != nil {
			t.Fatal(e)
		}
		if e = client.FS(ctx).Mkdir("/data/space ' folder"); e != nil {
			t.Fatal(e)
		}
		var out attachTestBuffer
		e = c.Executor().Stream(ctx, d.Ref, ExecOptions{Command: d.Command("/data/space ' folder", `printf '{"ok":true}' | jq -r .ok > shell-result; test -x /bin/bash; cat shell-result`, false), Stdout: &out, Stderr: &out})
		if e != nil || !d.PodToast || !strings.Contains(out.String(), "PodToast") || !strings.Contains(out.String(), "true") {
			t.Fatal(out.String(), e)
		}
		r, e := client.FS(ctx).Open("/data/space ' folder/shell-result")
		if e != nil {
			t.Fatal(e)
		}
		defer r.Close()
		b, e := io.ReadAll(r)
		if e != nil || string(b) != "true\n" {
			t.Fatal(string(b), e)
		}
	})
	metrics, metricsErr := client.Metrics(ctx)
	if metricsErr != nil || !strings.Contains(string(metrics), "podtoast_resources_available") {
		t.Fatalf("metrics endpoint: %s %v", metrics, metricsErr)
	}
	discovered, err := c.DiscoverMetrics(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	reader := &MetricsReader{Client: c, Ref: ref, UID: discovered.PodUID}
	defer reader.Close()
	checkedHTTP, checkedToast := false, false
	for _, ep := range discovered.Endpoints {
		if ep.Path != "/metrics" {
			continue
		}
		body, e := reader.Scrape(ctx, ep)
		if e != nil {
			t.Fatalf("scrape %s: %v", ep.ID, e)
		}
		samples, e := livemetrics.Parse(body)
		if e != nil || len(samples) == 0 {
			t.Fatalf("parse %s: %v", ep.ID, e)
		}
		if ep.Container != "" {
			checkedToast = true
		} else {
			checkedHTTP = true
		}
	}
	if !checkedHTTP || !checkedToast {
		t.Fatal("HTTP and PodToast endpoints were not both tested", discovered)
	}
	f := client.FS(ctx)
	start := time.Now()
	entries, err := f.ReadDir("/data")
	if err != nil || len(entries) < 2003 {
		t.Fatalf("listing %d %v", len(entries), err)
	}
	t.Logf("2003 entries: %s", time.Since(start))
	for _, path := range []string{"/data/hello.txt", "/data/absolute-link", "/data/relative-link"} {
		r, e := f.Open(path)
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(r)
		r.Close()
		if e != nil || string(b) != "hello from distroless\n" {
			t.Fatalf("%s: %q %v", path, b, e)
		}
	}
	payload := bytes.Repeat([]byte{0, 255, 1, 2}, 1<<18)
	w, err := f.Create("/data/upload")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	uploaded, err := f.Stat("/data/upload")
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := uploaded.(podtoast.Entry); !ok || e.UID != 65532 {
		t.Fatalf("new file did not retain application identity: %+v", uploaded)
	}
	if err = f.CopyFile("/data/upload", "/data/copy", ""); err != nil {
		t.Fatal(err)
	}
	if err = f.Rename("/data/copy", "/data/renamed"); err != nil {
		t.Fatal(err)
	}
	r, err := f.Open("/data/renamed")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	r.Close()
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal("copy corrupted", err)
	}
	if err = f.Chmod("/data/renamed", 0604); err != nil {
		t.Fatal(err)
	}
	st, err := f.Stat("/data/renamed")
	if err != nil || st.Mode().Perm() != 0604 {
		t.Fatal(st, err)
	}
	if err = f.Mkdir("/data/new"); err != nil {
		t.Fatal(err)
	}
	if err = f.Remove("/data/new", true); err != nil {
		t.Fatal(err)
	}
	w, err = f.Create("/forbidden-on-readonly-root")
	if err == nil {
		_, _ = w.Write([]byte("test"))
		err = w.Close()
	}
	if err == nil {
		t.Fatal("read-only filesystem write succeeded")
	}
	var snap podtoast.Snapshot
	if err = client.JSON(ctx, "GET", "/v1/snapshot", nil, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Processes.Value == nil || snap.Network.Value == nil {
		t.Fatalf("missing observations %+v", snap)
	}
	if snap.Resources.Unavailable != "" {
		t.Logf("resource metrics unavailable: %s", snap.Resources.Unavailable)
	} else {
		t.Log("verified target cgroup metrics available")
	}
	var result podtoast.ProbeResult
	if err = client.JSON(ctx, "POST", "/v1/probe", podtoast.Probe{Kind: "load", Target: "http://127.0.0.1:8080/", Requests: 10, Concurrency: 2, Rate: 100}, &result); err != nil || result.Completed != 10 || result.Failed != 0 {
		t.Fatalf("load %+v %v", result, err)
	}
	cancelCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- client.JSON(cancelCtx, "POST", "/v1/probe", podtoast.Probe{Kind: "http", Target: "http://127.0.0.1:8080/slow"}, &result)
	}()
	time.Sleep(100 * time.Millisecond)
	stop()
	select {
	case err = <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Esc cancellation blocked")
	}
	if _, err = f.ReadDir("/data"); err != nil {
		t.Fatal("browse after cancellation:", err)
	}
	// Reconnection reuses the already-running ephemeral container.
	other, err := c.StartPodToast(ctx, ref, image)
	if err != nil {
		t.Fatal(err)
	}
	other.Close()
	pod, err = c.Pod(ctx, ns, pod.Name)
	if err != nil || len(pod.Spec.EphemeralContainers) != 1 {
		t.Fatalf("agent not reused: %v", err)
	}
	// An arbitrary image uses its chosen argv, with no PodToast protocol.
	debug, err := c.StartEphemeral(ctx, ref, EphemeralOptions{Image: "podtoast-fixture:dev", Command: []string{"/fixture", "echo"}, Profile: "Restricted", RunAsUser: &uid, PullPolicy: "Never", ReadOnlyRoot: true, TimeoutSeconds: 20})
	if err != nil {
		t.Fatal(err)
	}
	attachCtx, stopAttach := context.WithTimeout(ctx, 2*time.Second)
	defer stopAttach()
	var echo attachTestBuffer
	input, writeInput := io.Pipe()
	defer input.Close()
	defer writeInput.Close()
	go func() {
		time.Sleep(200 * time.Millisecond)
		_, _ = io.WriteString(writeInput, "ephemeral-attach-test\n")
	}()
	err = c.Attacher(false).Stream(attachCtx, debug, ExecOptions{Stdin: input, Stdout: &echo})
	if !strings.Contains(echo.String(), "ephemeral-attach-test") {
		status, _ := c.Pod(ctx, ref.Namespace, ref.Pod)
		t.Fatalf("attach failed: %q %v status: %+v", echo.String(), err, status.Status.EphemeralContainerStatuses)
	}

	t.Run("generic ephemeral toolbox", func(t *testing.T) {
		rootUID := int64(0)
		debug, e := c.StartEphemeral(ctx, ref, EphemeralOptions{Image: image, Command: []string{"/bin/sh", "-c", "sleep 300"}, Profile: "Files and shell", RunAsUser: &rootUID, PullPolicy: "IfNotPresent", TimeoutSeconds: 30})
		if e != nil {
			t.Fatal(e)
		}
		d, e := c.ResolveDebugShell(ctx, ref, debug.Container)
		if e != nil {
			t.Fatal(e)
		}
		var out attachTestBuffer
		e = c.Executor().Stream(ctx, d.Ref, ExecOptions{Command: d.Command("/data", "printf generic > generic-file; cat generic-file", false), Stdout: &out, Stderr: &out})
		if e != nil || !strings.Contains(out.String(), "generic") {
			t.Fatal(out.String(), e)
		}
		fs, e := OpenContainerFS(ctx, c.Executor(), d.Ref, d.Probe)
		if e != nil {
			t.Fatal(e)
		}
		defer fs.Close()
		if _, e = fs.Lstat(d.Root + "/data/generic-file"); e != nil {
			t.Fatal(e)
		}
	})

	t.Run("explicit pod traffic control", func(t *testing.T) {
		network, e := c.OpenTraffic(ctx, ref, image, true)
		if e != nil {
			t.Fatal(e)
		}
		defer network.Close()
		var status podtoast.TrafficStatus
		config := podtoast.TrafficConfig{Direction: "outbound", Port: 8080, PeerCIDR: "127.0.0.0/8", DurationSeconds: 10, DelayMS: 100}
		if e = network.JSON(ctx, "POST", "/v1/traffic/start", config, &status); e != nil {
			t.Fatal(e)
		}
		var probe podtoast.ProbeResult
		if e = client.JSON(ctx, "POST", "/v1/probe", podtoast.Probe{Kind: "http", Target: "http://127.0.0.1:8080/metrics"}, &probe); e != nil || probe.ElapsedMS < 80 {
			t.Fatalf("traffic probe %+v %v", probe, e)
		}
		if e = network.JSON(ctx, "POST", "/v1/traffic/stop", nil, &status); e != nil || status.Active {
			t.Fatalf("traffic stop %+v %v", status, e)
		}
		config.Direction = "inbound"
		config.PeerCIDR = "0.0.0.0/0"
		if e = network.JSON(ctx, "POST", "/v1/traffic/start", config, &status); e != nil {
			t.Fatal(e)
		}
		start := time.Now()
		_, e = c.Core().CoreV1().RESTClient().Get().Namespace(ns).Resource("pods").Name(ref.Pod + ":8080").SubResource("proxy").Suffix("metrics").Do(ctx).Raw()
		if e != nil || time.Since(start) < 80*time.Millisecond {
			t.Fatalf("inbound redirect: %v %v", time.Since(start), e)
		}
		if e = network.JSON(ctx, "GET", "/v1/traffic", nil, &status); e != nil || status.Accepted == 0 {
			t.Fatalf("inbound stats: %+v %v", status, e)
		}
		if e = network.JSON(ctx, "POST", "/v1/traffic/stop", nil, &status); e != nil {
			t.Fatal(e)
		}
		// A dedicated network-only agent must not replace the filesystem debugger.
		if _, e = c.ResolveDebugShell(ctx, ref, ""); e != nil {
			t.Fatal(e)
		}
	})

	t.Run("shared PID agent reuse", func(t *testing.T) {
		shared := true
		spec := pod.Spec.DeepCopy()
		spec.ShareProcessNamespace = &shared
		spec.EphemeralContainers = nil
		spec.Containers = spec.Containers[:1]
		second := spec.Containers[0].DeepCopy()
		second.Name = "sidecar"
		second.Ports = nil
		second.Env = []corev1.EnvVar{{Name: "LISTEN_ADDRESS", Value: ":8081"}}
		second.VolumeMounts = []corev1.VolumeMount{{Name: "side-data", MountPath: "/data"}}
		spec.Containers = append(spec.Containers, *second)
		spec.Volumes = append(spec.Volumes, corev1.Volume{Name: "side-data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
		sp, err := c.Core().CoreV1().Pods(ns).Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: ns}, Spec: *spec}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for {
			sp, err = c.Pod(ctx, ns, sp.Name)
			if err != nil {
				t.Fatal(err)
			}
			running := 0
			for _, st := range sp.Status.ContainerStatuses {
				if st.State.Running != nil {
					running++
				}
			}
			if running == 2 {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(250 * time.Millisecond):
			}
		}
		firstRef := ContainerRef{Namespace: ns, Pod: sp.Name, Container: "app"}
		first, err := c.StartPodToast(ctx, firstRef, image)
		if err != nil {
			t.Fatal(err)
		}
		defer first.Close()
		secondRef := firstRef
		secondRef.Container = "sidecar"
		secondClient, err := c.StartPodToast(ctx, secondRef, image)
		if err != nil {
			t.Fatal(err)
		}
		defer secondClient.Close()
		sp, err = c.Pod(ctx, ns, sp.Name)
		if err != nil {
			t.Fatal(err)
		}
		if len(sp.Spec.EphemeralContainers) != 1 {
			t.Fatal("expected one shared agent", len(sp.Spec.EphemeralContainers))
		}
		if err = first.FS(ctx).Mkdir("/data/only-in-first"); err != nil {
			t.Fatal(err)
		}
		if _, err = secondClient.FS(ctx).Lstat("/data/only-in-first"); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("target roots crossed", err)
		}
		if _, err = secondClient.FS(ctx).Lstat("/data/hello.txt"); err != nil {
			t.Fatal(err)
		}
		t.Log("one ephemeral agent, two distinct target filesystems")
	})
}

type attachTestBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *attachTestBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *attachTestBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }
