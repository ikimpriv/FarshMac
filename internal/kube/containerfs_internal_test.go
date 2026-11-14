package kube

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseLsOutputVariants(t *testing.T) {
	now := time.Date(2026, 2, 1, 0, 0, 0, 0, time.Local)
	out := "total 8\n" +
		"drwxr-xr-x   2 0 0 4096 Jan  5 12:00 name with spaces\n" +
		"lrwxrwxrwx   1 0 0    3 Dec 31  2023 link -> some target\n" +
		"-rwsr-xr-x@  1 0 0  120 Nov 30 09:15 setuid\n" +
		"crw-rw-rw-   1 0 0   1,  3 Jan  1 00:00 null\n" +
		"-rw-r--r--   1 0 0    0 Jan  5 12:00 a -> b.txt\n"
	infos, err := parseLsOutput([]byte(out), now)
	if err != nil || len(infos) != 4 {
		t.Fatalf("%d entries %v", len(infos), err)
	}
	if infos[0].name != "name with spaces" || !infos[0].IsDir() || infos[0].modTime.Year() != 2026 || infos[0].modTime.Month() != 1 {
		t.Fatalf("%+v", infos[0])
	}
	if infos[1].name != "link" || infos[1].target != "some target" || infos[1].mode&os.ModeSymlink == 0 || infos[1].modTime.Year() != 2023 {
		t.Fatalf("%+v", infos[1])
	}
	if infos[2].name != "setuid" || infos[2].mode&os.ModeSetuid == 0 || infos[2].mode.Perm() != 0755 || infos[2].modTime.Year() != 2025 {
		t.Fatalf("%+v", infos[2])
	}
	if infos[3].name != "a -> b.txt" || infos[3].target != "" {
		t.Fatalf("%+v", infos[3])
	}
}

func TestPodSummary(t *testing.T) {
	pod := samplePod()
	s := PodSummaryOf(pod)
	if s.Phase != "Running" || s.Node != "node-1" || len(s.Containers) != 3 {
		t.Fatalf("%+v", s)
	}
	if s.Containers[0].Kind != "init" || s.Containers[0].State != "terminated:0" {
		t.Fatalf("init %+v", s.Containers[0])
	}
	if s.Containers[1].Kind != "" || s.Containers[1].State != "running" || !s.Containers[1].Ready || s.Containers[1].Started.IsZero() {
		t.Fatalf("app %+v", s.Containers[1])
	}
	if s.Containers[2].Kind != "ephemeral" || s.Containers[2].State != "waiting:CrashLoopBackOff" {
		t.Fatalf("debug %+v", s.Containers[2])
	}
	if len(s.Regular()) != 1 || s.Regular()[0].Name != "app" {
		t.Fatal("regular", s.Regular())
	}
	if _, ok := s.SingleContainer(); ok {
		t.Fatal("init containers must prevent the single-container shortcut")
	}
	pod.Spec.InitContainers, pod.Spec.EphemeralContainers = nil, nil
	if name, ok := PodSummaryOf(pod).SingleContainer(); !ok || name != "app" {
		t.Fatal("single container", name, ok)
	}
	if got := s.Names(); strings.Join(got, ",") != "app,debug,setup" {
		t.Fatal("names", got)
	}
}
