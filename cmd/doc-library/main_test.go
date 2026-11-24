package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	doclibrary "doc-library/pkg/doclibrary"
)

func TestCLIChild(t *testing.T) {
	if os.Getenv("DOC_LIBRARY_TEST_CHILD") != "1" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("DOC_LIBRARY_TEST_ARGS")), &args); err != nil {
		os.Exit(2)
	}
	os.Exit(run(args, os.Stdin, os.Stdout, os.Stderr))
}

func TestDecodeRequestOverridesExplicitZeroValues(t *testing.T) {
	defaults := doclibrary.Request{Mode: "search", Query: "flag", Limit: 9, Regex: true, CaseSensitive: true, Kinds: []string{"source"}, Extensions: []string{".md", ".go"}}
	got, id, err := decodeRequest(strings.NewReader(`{"arguments":{"query":"json","limit":0,"regex":false,"case_sensitive":false,"kinds":["pdf"],"extensions":[".pdf"]},"request_id":"r-1"}`), defaults)
	if err != nil {
		t.Fatal(err)
	}
	if id != "r-1" || got.Query != "json" || got.Limit != 0 || got.Regex || got.CaseSensitive || got.Mode != "search" || !reflect.DeepEqual(got.Kinds, []string{"pdf"}) || !reflect.DeepEqual(got.Extensions, []string{".pdf"}) {
		t.Fatalf("unexpected merged request: id=%q request=%+v", id, got)
	}
}

func TestDecodeRequestRejectsUnknownAndMultipleJSON(t *testing.T) {
	for _, input := range []string{`null`, `{"wat":1}`, `{"mode":"schema"} {"mode":"schema"}`, `{"arguments":null}`, `{"arguments":{"wat":1}}`, `{"source":"a","sources":["b"]}`} {
		if _, _, err := decodeRequest(strings.NewReader(input), doclibrary.Request{}); err == nil {
			t.Errorf("accepted invalid request %s", input)
		}
	}
}

func TestDecodeRequestSourceReplacesCLISelector(t *testing.T) {
	defaults := doclibrary.Request{Sources: []string{"cli-source"}}
	got, _, err := decodeRequest(strings.NewReader(`{"source":"json-source"}`), defaults)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "json-source" || len(got.Sources) != 0 {
		t.Fatalf("source override retained CLI defaults: %+v", got)
	}
}

func TestParseRegion(t *testing.T) {
	r, err := parseRegion("1, 2, 90, 95")
	if err != nil || r == nil || r.Right != 90 {
		t.Fatalf("parse region: %#v, %v", r, err)
	}
	if _, err := parseRegion("1,2,no,4"); err == nil {
		t.Fatal("accepted invalid region")
	}
}

func TestReadJSONLineContinuesAfterOversizedRecord(t *testing.T) {
	input := strings.NewReader(strings.Repeat("x", maxJSONLine+7) + "\n{\"mode\":\"schema\"}\n")
	reader := bufio.NewReaderSize(input, 64*1024)
	line, err, done := readJSONLine(reader)
	if err == nil || !strings.Contains(err.Error(), "exceeds") || len(line) != 0 || done {
		t.Fatalf("oversized record: len=%d err=%v done=%v", len(line), err, done)
	}
	line, err, done = readJSONLine(reader)
	if err != nil || done || strings.TrimSpace(string(line)) != `{"mode":"schema"}` {
		t.Fatalf("following record: %q err=%v done=%v", line, err, done)
	}
}

func TestCLIProcessArgumentErrorsAreJSON(t *testing.T) {
	cmd := cliProcess(t, []string{"--not-a-flag"}, "")
	if cmd.ProcessState.ExitCode() != 2 {
		t.Fatalf("exit=%d stderr=%s", cmd.ProcessState.ExitCode(), cmd.Stderr)
	}
	var result map[string]any
	if err := json.Unmarshal(cmd.Stdout, &result); err != nil || result["ok"] != false {
		t.Fatalf("expected structured JSON error, got %q (%v)", cmd.Stdout, err)
	}
	config := cliProcess(t, []string{"--config", filepath.Join(t.TempDir(), "missing.json"), "schema"}, "")
	if config.ProcessState.ExitCode() != 2 {
		t.Fatalf("config error exit=%d", config.ProcessState.ExitCode())
	}
	if err := json.Unmarshal(config.Stdout, &result); err != nil || result["ok"] != false {
		t.Fatalf("config error was not JSON: %q (%v)", config.Stdout, err)
	}
}

func TestCLIProcessSchemaWithoutRoots(t *testing.T) {
	cmd := cliProcess(t, []string{"--mode", "schema"}, "")
	if cmd.ProcessState.ExitCode() != 0 {
		t.Fatalf("exit=%d stderr=%s", cmd.ProcessState.ExitCode(), cmd.Stderr)
	}
	var result map[string]any
	if err := json.Unmarshal(cmd.Stdout, &result); err != nil {
		t.Fatalf("stdout is not JSON: %q (%v)", cmd.Stdout, err)
	}
	if result["ok"] != true {
		t.Fatalf("schema response not ok: %s", cmd.Stdout)
	}
	positional := cliProcess(t, []string{"schema", "--limit", "0"}, "")
	if positional.ProcessState.ExitCode() != 0 {
		t.Fatalf("positional mode with trailing flag failed: %s", positional.Stderr)
	}
}

func TestCLIProcessJSONLContinuesAfterBadLine(t *testing.T) {
	cmd := cliProcess(t, []string{"--jsonl"}, `{"mode":"schema"}`+"\n"+`{"unexpected":true}`+"\n"+`{"mode":"schema"}`+"\n")
	if cmd.ProcessState.ExitCode() != 1 {
		t.Fatalf("exit=%d stderr=%s stdout=%s", cmd.ProcessState.ExitCode(), cmd.Stderr, cmd.Stdout)
	}
	lines := strings.Split(strings.TrimSpace(string(cmd.Stdout)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected one result per input line, got %d: %s", len(lines), cmd.Stdout)
	}
	for i, line := range lines {
		var value map[string]any
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			t.Errorf("line %d invalid JSON: %v", i+1, err)
		}
	}
	if !strings.Contains(lines[1], `"ok":false`) {
		t.Fatalf("bad line did not produce structured error: %s", lines[1])
	}
}

func TestCLIProcessConfigRelativeToConfigFromDifferentCWD(t *testing.T) {
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, "cfg")
	workDir := filepath.Join(tmp, "other")
	root := filepath.Join(configDir, "docs")
	for _, dir := range []string{configDir, workDir, root} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := `{"cache_dir":"cache","sources":[{"id":"fixture","root":"docs"}]}`
	path := filepath.Join(configDir, "library.json")
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := cliProcessAt(t, []string{"--config", path, "schema"}, "", workDir)
	if cmd.ProcessState.ExitCode() != 0 {
		t.Fatalf("exit=%d stderr=%s stdout=%s", cmd.ProcessState.ExitCode(), cmd.Stderr, cmd.Stdout)
	}
	var result map[string]any
	if err := json.Unmarshal(cmd.Stdout, &result); err != nil {
		t.Fatalf("stdout is not JSON: %q (%v)", cmd.Stdout, err)
	}
}

func TestCLIProcessStdinAndRequestFile(t *testing.T) {
	stdin := cliProcess(t, []string{"--stdin"}, `{"mode":"schema"}`)
	if stdin.ProcessState.ExitCode() != 0 {
		t.Fatalf("stdin exit=%d stderr=%s stdout=%s", stdin.ProcessState.ExitCode(), stdin.Stderr, stdin.Stdout)
	}
	var result map[string]any
	if err := json.Unmarshal(stdin.Stdout, &result); err != nil || result["ok"] != true {
		t.Fatalf("stdin response invalid: %s (%v)", stdin.Stdout, err)
	}

	request := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(request, []byte(`{"mode":"schema"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	file := cliProcess(t, []string{"--request", request}, "")
	if file.ProcessState.ExitCode() != 0 {
		t.Fatalf("request exit=%d stderr=%s stdout=%s", file.ProcessState.ExitCode(), file.Stderr, file.Stdout)
	}
	if err := json.Unmarshal(file.Stdout, &result); err != nil || result["ok"] != true {
		t.Fatalf("request response invalid: %s (%v)", file.Stdout, err)
	}
}

func TestCLIProcessInputModesAreExclusive(t *testing.T) {
	cmd := cliProcess(t, []string{"--stdin", "--jsonl"}, `{}`)
	if cmd.ProcessState.ExitCode() != 2 {
		t.Fatalf("expected exit 2, got %d", cmd.ProcessState.ExitCode())
	}
}

func TestRootListAndRepeatableSourceFlags(t *testing.T) {
	var roots rootList
	_ = roots.Set("a=/tmp/a")
	_ = roots.Set("/tmp/b")
	if !reflect.DeepEqual([]string(roots), []string{"a=/tmp/a", "/tmp/b"}) {
		t.Fatalf("roots=%v", roots)
	}
}

type processResult struct {
	Stdout, Stderr []byte
	ProcessState   *os.ProcessState
}

func cliProcess(t *testing.T, args []string, input string) processResult {
	return cliProcessAt(t, args, input, t.TempDir())
}
func cliProcessAt(t *testing.T, args []string, input, dir string) processResult {
	t.Helper()
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCLIChild$")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DOC_LIBRARY_TEST_CHILD=1", "DOC_LIBRARY_TEST_ARGS="+string(encoded), "DOC_LIBRARY_CACHE="+filepath.Join(t.TempDir(), "cache"))
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if exit, ok := err.(*exec.ExitError); ok {
		return processResult{stdout.Bytes(), stderr.Bytes(), exit.ProcessState}
	}
	if err != nil {
		t.Fatal(err)
	}
	return processResult{stdout.Bytes(), stderr.Bytes(), cmd.ProcessState}
}
