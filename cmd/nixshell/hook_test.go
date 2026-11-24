package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	in := envState{Project: "/tmp/project", Prev: map[string]*string{"FOO": ptr("bar"), "BAZ": nil}}
	out := decodeState(encodeState(in))
	if out.Project != in.Project || len(out.Prev) != 2 {
		t.Fatalf("state = %#v, want %#v", out, in)
	}
	if out.Prev["FOO"] == nil || *out.Prev["FOO"] != "bar" || out.Prev["BAZ"] != nil {
		t.Fatalf("state values = %#v", out.Prev)
	}
}

func TestHookSnippets(t *testing.T) {
	for _, tc := range []struct {
		shell string
		want  []string
	}{
		{"bash", []string{"hook-env bash", "PROMPT_COMMAND"}},
		{"zsh", []string{"hook-env zsh", "precmd_functions"}},
		{"fish", []string{"hook-env fish", "--on-variable PWD"}},
	} {
		out := captureStdout(t, func() error { return printHook(tc.shell) })
		for _, want := range tc.want {
			if !strings.Contains(out, want) {
				t.Errorf("hook %s does not contain %q: %s", tc.shell, want, out)
			}
		}
	}
}

func captureStdout(t *testing.T, f func() error) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	err = f()
	os.Stdout = old
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
