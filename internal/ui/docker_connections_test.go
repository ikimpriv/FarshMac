package ui

import (
	"context"
	"farshmac/internal/docker"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDockerContextFormAndPickers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".ssh")
	os.Mkdir(dir, 0700)
	os.WriteFile(filepath.Join(dir, "config"), []byte("Host build other\nHost * !excluded\nInclude hosts/*\n"), 0600)
	os.Mkdir(filepath.Join(dir, "hosts"), 0700)
	os.WriteFile(filepath.Join(dir, "hosts", "more"), []byte("Host extra\nInclude config\n"), 0600)
	os.WriteFile(filepath.Join(dir, "id_fixture"), []byte("fixture"), 0600)
	if got := dockerSSHAliases(filepath.Join(dir, "config")); !reflect.DeepEqual(got, []string{"build", "extra", "other"}) {
		t.Fatal(got)
	}
	a, e := New(t.TempDir(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	a.dockerContextForm(0, true)
	form := a.Form
	form.Focus = 1
	a.formKey(Message{Key: "F4"})
	if a.Form.Title != "SSH alias" {
		t.Fatal(a.Form)
	}
	a.formKey(Message{Key: "Enter"})
	if a.Form != form || a.Form.Fields[1].Value != "build" {
		t.Fatal(a.Form)
	}
	a.dockerContextForm(0, false)
	form = a.Form
	form.Focus = 4
	a.formKey(Message{Key: "F4"})
	waitNetwork(t, a)
	if !strings.HasPrefix(a.Form.Title, "SSH key") {
		t.Fatal(a.Form)
	}
	for i, n := range a.Form.Choice {
		if n == "id_fixture" {
			a.Form.Focus = i
			break
		}
	}
	a.formKey(Message{Key: "Enter"})
	if a.Form != form || form.Fields[4].Value != filepath.Join(dir, "id_fixture") {
		t.Fatal(a.Form)
	}
	// Invalid submission retains the key and other entered values.
	form.Fields[0].Value = "bad/name"
	form.Fields[1].Value = "192.0.2.1"
	form.Focus = len(form.Fields)
	a.formKey(Message{Key: "Enter"})
	if a.Form == nil || a.Form.Fields[4].Value != filepath.Join(dir, "id_fixture") || a.Busy {
		t.Fatal("validation lost form", a.Form)
	}
}

func TestDockerRefreshKeepsReplacementToolbox(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		dir := t.TempDir()
		binary := filepath.Join(dir, "docker")
		rows := []string{`{"ID":"old","State":"running","Labels":"io.farshmac.podtoast.target=target"}`, `{"ID":"new","State":"running","Labels":"io.farshmac.podtoast.target=target,io.farshmac.podtoast.replaces=old"}`}
		if reverse {
			rows[0], rows[1] = rows[1], rows[0]
		}
		if e := os.WriteFile(binary, []byte("#!/bin/sh\ncat <<'DATA'\n"+strings.Join(rows, "\n")+"\nDATA\n"), 0700); e != nil {
			t.Fatal(e)
		}
		d := &dockerPanel{client: docker.Client{Binary: binary}, helpers: map[string]string{"target": "old"}}
		if e := d.refresh(context.Background()); e != nil {
			t.Fatal(e)
		}
		if d.helper("target") != "new" {
			t.Fatal("selected obsolete toolbox", d.helpers)
		}
	}
}
