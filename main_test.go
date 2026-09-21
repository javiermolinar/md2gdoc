package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCLI(t *testing.T) {
	cases := []struct {
		name                           string
		args                           []string
		input, contains, errorContains string
	}{
		{"stdin", nil, "# Title\n\nBody", `"text": "Title\nBody\n"`, ""},
		{"compact array", []string{"-", "--requests-only", "--compact"}, "hello", `[{"insertText":`, ""},
		{"explicit requests", []string{"--format", "requests"}, "# Title", `"text": "Title\n"`, ""},
		{"HTML", []string{"--format", "html"}, "# Title\n\n| A |\n|---|\n| B |", "<table style=", ""},
		{"tab and offset", []string{"--start-index", "42", "--tab-id", "t.tables", "--requests-only", "--compact"}, "| A |\n|---|\n| **😀** |", `"location":{"index":42,"tabId":"t.tables"}`, ""},
		{"empty", []string{"--compact"}, "", `{"requests":[]}`, ""},
		{"local link text", []string{"--relative-links", "text"}, "[Notes](./notes.md)", `Notes (./notes.md)`, ""},
		{"local link HTML", []string{"--format", "html", "--relative-links=text"}, "[Notes](#here)", `Notes (#here)`, ""},
		{"invalid link mode", []string{"--relative-links=drop"}, "", "", "--relative-links"},
		{"empty link mode", []string{"--relative-links="}, "", "", "--relative-links"},
		{"help", []string{"--help"}, "", "Usage: md2gdoc", ""},
		{"short help", []string{"-h"}, "", "Makes no API calls", ""},
		{"version", []string{"--version"}, "", buildVersion(), ""},
		{"equals syntax", []string{"--format=html"}, "# Title", "<h1>Title</h1>", ""},
		{"unknown option", []string{"--unknown"}, "hello", "", "unknown option"},
		{"two files", []string{"one.md", "two.md"}, "", "", "at most one"},
		{"fractional index", []string{"--start-index", "1.5"}, "hello", "", "--start-index"},
		{"negative index", []string{"--start-index", "-1"}, "hello", "", "--start-index"},
		{"zero index", []string{"--start-index", "0"}, "hello", "", "--start-index"},
		{"overflowing index", []string{"--start-index", "999999999999999999999"}, "hello", "", "--start-index"},
		{"index exceeds API", []string{"--start-index=2147483648"}, "hello", "", "--start-index"},
		{"empty index", []string{"--start-index="}, "hello", "", "--start-index"},
		{"leading zero", []string{"--start-index=01"}, "hello", "", "--start-index"},
		{"leading plus", []string{"--start-index=+1"}, "hello", "", "--start-index"},
		{"missing tab", []string{"--tab-id"}, "", "", "requires a value"},
		{"missing tab before flag", []string{"--tab-id", "--compact"}, "", "", "requires a value"},
		{"empty tab", []string{"--tab-id", ""}, "hello", "", "tabId"},
		{"missing file", []string{"/no/such/markdown-file.md"}, "", "", "markdown-file.md"},
		{"unsupported image", nil, "# Valid\n\n![image](https://example.com/i.png)", "", "images"},
		{"bad second table", nil, "| A |\n|---|\n| B |\n\n| C |\n|---|\n| [bad](./relative) |", "", "link target"},
		{"invalid format", []string{"--format", "docx"}, "text", "", "--format"},
		{"empty format", []string{"--format", ""}, "text", "", "--format"},
		{"missing format", []string{"--format"}, "text", "", "requires a value"},
		{"HTML with index", []string{"--format", "html", "--start-index", "1"}, "text", "", "require --format requests"},
		{"HTML with tab", []string{"--format", "html", "--tab-id", "t.0"}, "text", "", "require --format requests"},
		{"HTML array", []string{"--format", "html", "--requests-only"}, "text", "", "require --format requests"},
		{"HTML compact", []string{"--format", "html", "--compact"}, "text", "", "require --format requests"},
		{"HTML invalid image", []string{"--format", "html"}, "# Valid\n\n![local](./secret.png)", "", "image target"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			err := execute(tc.args, strings.NewReader(tc.input), &stdout, false)
			if tc.errorContains != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errorContains) {
					t.Fatalf("expected %q, got %v", tc.errorContains, err)
				}
				if stdout.Len() != 0 {
					t.Fatalf("partial output on error: %q", stdout.String())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout.String(), tc.contains) {
				t.Fatalf("missing %q in %s", tc.contains, stdout.String())
			}
			if strings.HasPrefix(stdout.String(), "[") || strings.HasPrefix(stdout.String(), "{") {
				if !json.Valid(stdout.Bytes()) {
					t.Error("invalid JSON output")
				}
			}
		})
	}
}

func TestCLIFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "input.md")
	if err := os.WriteFile(file, []byte("**bold**"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := execute([]string{file, "--start-index", "10", "--tab-id", "t.0"}, nil, &out, true); err != nil {
		t.Fatal(err)
	}
	var body batchUpdateBody
	if err := json.Unmarshal(out.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if got := body.Requests[0].InsertText.Location; got.Index != 10 || got.TabID != "t.0" {
		t.Fatalf("wrong location: %+v", got)
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "**bold**" {
		t.Fatal("input file changed")
	}
}

func TestCLIOptionSeparator(t *testing.T) {
	o, err := parseArgs([]string{"--", "-notes.md"})
	if err != nil || o.file != "-notes.md" {
		t.Fatalf("got %+v, %v", o, err)
	}
}

func TestCLIEmptyInputFile(t *testing.T) {
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	var out bytes.Buffer
	if err := execute([]string{"--compact"}, stdin, &out, false); err != nil {
		t.Fatal(err)
	}
	if out.String() != "{\"requests\":[]}\n" {
		t.Fatalf("unexpected output: %s", &out)
	}
}

type unreadInput struct{ t *testing.T }

func (r unreadInput) Read([]byte) (int, error) {
	r.t.Fatal("interactive invocation must not wait for stdin")
	return 0, io.EOF
}

func TestCLIInteractiveInput(t *testing.T) {
	for _, args := range [][]string{nil, {"--format", "html"}, {"--compact"}} {
		var out bytes.Buffer
		if err := execute(args, unreadInput{t}, &out, true); err != nil {
			t.Fatal(err)
		}
		if out.String() != help {
			t.Fatalf("args %v: expected help, got %q", args, &out)
		}
	}
	var out bytes.Buffer
	if err := execute([]string{"-", "--compact"}, strings.NewReader("Hello"), &out, true); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out.Bytes()) || !strings.Contains(out.String(), `"text":"Hello\n"`) {
		t.Fatalf("explicit '-' should still read interactive stdin: %s", &out)
	}
}

func TestBinary(t *testing.T) {
	name := "md2gdoc"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %s: %v", out, err)
	}
	for _, invalid := range []bool{false, true} {
		cmd := exec.Command(binary, "--compact")
		input := "# Hello"
		if invalid {
			input = "![image](https://example.com/i.png)"
		}
		cmd.Stdin = strings.NewReader(input)
		var out, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		err := cmd.Run()
		if invalid {
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 1 || out.Len() != 0 || !strings.HasPrefix(stderr.String(), "md2gdoc: ") {
				t.Fatalf("bad failure: %v stdout=%s stderr=%s", err, &out, &stderr)
			}
		} else if err != nil || !json.Valid(out.Bytes()) || stderr.Len() != 0 {
			t.Fatalf("bad success: %v stdout=%s stderr=%s", err, &out, &stderr)
		}
	}
}
