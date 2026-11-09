package highlight

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == HelperFlag {
		if os.Args[2] == "__timeout.go" {
			time.Sleep(10 * time.Second)
		}
		os.Exit(HelperMain(os.Args[2]))
	}
	os.Exit(m.Run())
}
func TestLanguagesUnicodeAndMultiline(t *testing.T) {
	for _, tc := range []struct{ path, text, needle string }{
		{"main.go", "package main\n/* первая\nвторая */\nvar s = `много\nстрок`", "вторая"},
		{"a.py", "# привет\ns = '''hello\n世界'''\nprint(s)", "世界"},
		{"a.json", "{\"ключ\": true, \"number\": 123}", "ключ"},
		{"a.sh", "#!/bin/sh\necho \"hello\"", "hello"},
		{"a.js", "const s = \"hello\"; // comment", "hello"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			r, err := Parse(tc.path, tc.text)
			if err != nil {
				t.Fatal(err)
			}
			if r.Language == "" || r.Color(strings.Index(tc.text, tc.needle)) == Plain {
				t.Fatalf("missing color: %+v", r)
			}
			if r.Color(len(tc.text)) != Plain {
				t.Fatal("color beyond EOF")
			}
		})
	}
}
func TestHelperRoundTripAndCancel(t *testing.T) {
	r, err := Run(context.Background(), "main.go", "package main\nvar x = 42")
	if err != nil || r.Language != "Go" || len(r.Spans) == 0 {
		t.Fatalf("%+v %v", r, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, err = Run(ctx, "main.go", "package main"); err == nil {
		t.Fatal("cancel ignored")
	}
	if time.Since(start) > time.Second {
		t.Fatal("slow cancellation")
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	if _, err = Run(ctx, "main.go", "package main"); err == nil {
		t.Fatal("deadline ignored")
	}
	if len(slots) != 0 {
		t.Fatal("helper slot leaked")
	}
}
func TestLimitsAndUnknown(t *testing.T) {
	if _, err := Parse("a.go", strings.Repeat("x", MaxBytes+1)); err == nil {
		t.Fatal("size cap ignored")
	}
	if _, err := Parse("a.go", strings.Repeat("x ", MaxSpans*2+1)); err == nil {
		t.Fatal("token cap ignored")
	}
	r, err := Parse("unknown.zzzz", "hello world")
	if err != nil || len(r.Spans) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	b := &limitedOutput{}
	if _, err := b.Write(make([]byte, (8<<20)+1)); err == nil {
		t.Fatal("output cap ignored")
	}
}

func TestUncooperativeHelperIsKilled(t *testing.T) {
	start := time.Now()
	if _, err := Run(context.Background(), "__timeout.go", "package main"); err == nil {
		t.Fatal("timeout ignored")
	}
	if elapsed := time.Since(start); elapsed < Timeout || elapsed > Timeout+time.Second {
		t.Fatalf("deadline duration %v", elapsed)
	}
	if len(slots) != 0 {
		t.Fatal("slot leaked after kill")
	}
}
