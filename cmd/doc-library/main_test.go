package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
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

func TestCLIOutputVersionsAndBBoxOverrides(t *testing.T) {
	root := filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("unique output version fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	baseArgs := []string{"--root", "fixture=" + root, "--mode", "search", "--query", "unique"}

	legacy := cliProcess(t, baseArgs, "")
	if legacy.ProcessState.ExitCode() != 0 {
		t.Fatalf("legacy exit=%d stdout=%s stderr=%s", legacy.ProcessState.ExitCode(), legacy.Stdout, legacy.Stderr)
	}
	var v1 map[string]any
	if err := json.Unmarshal(legacy.Stdout, &v1); err != nil {
		t.Fatal(err)
	}
	if v1["version"] != "doc-library/1" || v1["text"] == nil || v1["cache"] == nil {
		t.Fatalf("legacy response changed: %s", legacy.Stdout)
	}

	compactArgs := append(append([]string(nil), baseArgs...), "--output-version=2")
	compact := cliProcess(t, compactArgs, "")
	if compact.ProcessState.ExitCode() != 0 {
		t.Fatalf("compact exit=%d stdout=%s stderr=%s", compact.ProcessState.ExitCode(), compact.Stdout, compact.Stderr)
	}
	var v2 map[string]any
	if err := json.Unmarshal(compact.Stdout, &v2); err != nil {
		t.Fatal(err)
	}
	results, ok := v2["results"].([]any)
	if !ok || len(results) != 1 || v2["version"] != "doc-library/2" || v2["text"] != nil || v2["cache"] != nil {
		t.Fatalf("compact response should contain body once and omit diagnostics: %s", compact.Stdout)
	}
	if results[0].(map[string]any)["text"] != "unique output version fixture" {
		t.Fatalf("unexpected compact result body: %s", compact.Stdout)
	}

	// JSON false overrides an explicit CLI true; JSON true overrides false.
	for _, tc := range []struct {
		cli  string
		json string
		want bool
	}{{"true", `false`, false}, {"false", `true`, true}} {
		args := append(append([]string(nil), baseArgs...), "--output-version=2", "--include-bbox="+tc.cli, "--stdin")
		got := cliProcess(t, args, `{"include_bbox":`+tc.json+`}`)
		if got.ProcessState.ExitCode() != 0 {
			t.Fatalf("bbox override exit=%d stdout=%s stderr=%s", got.ProcessState.ExitCode(), got.Stdout, got.Stderr)
		}
		var response map[string]any
		if err := json.Unmarshal(got.Stdout, &response); err != nil {
			t.Fatal(err)
		}
		// The source fixture has no PDF geometry; the request still exercises merge semantics.
		if response["version"] != "doc-library/2" {
			t.Fatalf("wrong format after bbox override: %s", got.Stdout)
		}
	}
}

func TestCLIConfigOutputVersionAndExplicitOverride(t *testing.T) {
	root := filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("configured output fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(t.TempDir(), "library.yaml")
	body := "sources:\n  - id: fixture\n    root: " + root + "\noutput:\n  version: 2\n  include_bbox: true\n"
	if err := os.WriteFile(config, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--config", config, "--mode", "search", "--query", "configured"}
	compact := cliProcess(t, args, "")
	if compact.ProcessState.ExitCode() != 0 {
		t.Fatalf("config output failed: %s %s", compact.Stdout, compact.Stderr)
	}
	var response map[string]any
	if err := json.Unmarshal(compact.Stdout, &response); err != nil || response["version"] != "doc-library/2" {
		t.Fatalf("config output.version was not applied: %s (%v)", compact.Stdout, err)
	}
	legacy := cliProcess(t, append(args, "--output-version=1"), "")
	if legacy.ProcessState.ExitCode() != 0 {
		t.Fatalf("CLI override failed: %s %s", legacy.Stdout, legacy.Stderr)
	}
	if err := json.Unmarshal(legacy.Stdout, &response); err != nil || response["version"] != "doc-library/1" {
		t.Fatalf("CLI output-version did not override config: %s (%v)", legacy.Stdout, err)
	}
}

func TestCLIRequestOutputVersionZeroInheritsConfig(t *testing.T) {
	root := filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(t.TempDir(), "library.yaml")
	writeConfig := func(version int) {
		t.Helper()
		body := "sources:\n  - id: fixture\n    root: " + root + "\noutput:\n  version: " + strconv.Itoa(version) + "\n"
		if err := os.WriteFile(config, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runRequest := func(args []string) map[string]any {
		t.Helper()
		cmd := cliProcess(t, args, `{"mode":"schema","output_version":0}`)
		if cmd.ProcessState.ExitCode() != 0 {
			t.Fatalf("request zero should inherit startup output version: %s %s", cmd.Stdout, cmd.Stderr)
		}
		var response map[string]any
		if err := json.Unmarshal(cmd.Stdout, &response); err != nil {
			t.Fatal(err)
		}
		return response
	}

	writeConfig(2)
	fromConfig := runRequest([]string{"--config", config, "--stdin"})
	if fromConfig["version"] != "doc-library/2" {
		t.Fatalf("request output_version=0 did not inherit config v2: %#v", fromConfig["version"])
	}

	writeConfig(1)
	fromCLI := runRequest([]string{"--config", config, "--output-version=2", "--stdin"})
	if fromCLI["version"] != "doc-library/2" {
		t.Fatalf("request output_version=0 did not inherit CLI v2 over config v1: %#v", fromCLI["version"])
	}
}

func TestCLIIncludeBBoxBareFlagMeansTrue(t *testing.T) {
	cmd := cliProcess(t, []string{"--mode", "schema", "--output-version=2", "--include-bbox"}, "")
	if cmd.ProcessState.ExitCode() != 0 {
		t.Fatalf("bare --include-bbox should parse as true: exit=%d stdout=%s stderr=%s", cmd.ProcessState.ExitCode(), cmd.Stdout, cmd.Stderr)
	}
}

func TestCLIRejectsInvalidOutputVersionFlags(t *testing.T) {
	for _, value := range []string{"0", "3"} {
		cmd := cliProcess(t, []string{"--mode", "schema", "--output-version=" + value}, "")
		if cmd.ProcessState.ExitCode() == 0 {
			t.Errorf("accepted invalid output version %s: %s", value, cmd.Stdout)
		}
	}
}

func TestCLIProcessJSONLUsesPerLineOutputVersion(t *testing.T) {
	cmd := cliProcess(t, []string{"--mode", "search", "--root", "fixture=" + t.TempDir(), "--output-version=2", "--jsonl"}, `{"mode":"schema","output_version":1}`+"\n"+`{"mode":"schema","output_version":2}`+"\n")
	if cmd.ProcessState.ExitCode() != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", cmd.ProcessState.ExitCode(), cmd.Stdout, cmd.Stderr)
	}
	lines := strings.Split(strings.TrimSpace(string(cmd.Stdout)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two independent responses: %s", cmd.Stdout)
	}
	var first, second map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatal(err)
	}
	if first["version"] != "doc-library/1" || first["cache"] == nil || second["version"] != "doc-library/2" || second["cache"] != nil {
		t.Fatalf("JSONL format selection leaked between lines: %s", cmd.Stdout)
	}
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

func TestApplyOutputOptionsSetsStartupConfigDefaults(t *testing.T) {
	configured := false
	base := doclibrary.Config{Output: doclibrary.OutputSettings{Version: 1, IncludeBBox: &configured}}
	got := applyOutputOptions(base, 2, true, true, true)
	if got.Output.Version != 2 || got.Output.IncludeBBox == nil || !*got.Output.IncludeBBox {
		t.Fatalf("explicit CLI settings did not override startup config: %+v", got.Output)
	}
	request, _, err := decodeRequest(strings.NewReader(`{"output_version":0,"include_bbox":null}`), doclibrary.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if request.OutputVersion != 0 || request.IncludeBBox != nil {
		t.Fatalf("zero/null request settings should inherit startup config: %+v", request)
	}
	// Core resolution receives CLI values in config and no request override.
	if got.Output.Version != 2 || *got.Output.IncludeBBox != true || request.OutputVersion != 0 || request.IncludeBBox != nil {
		t.Fatalf("request inheritance lost CLI startup defaults: config=%+v request=%+v", got.Output, request)
	}
}

func TestDecodeRequestBBoxOverrideAndNullInheritance(t *testing.T) {
	configured := true
	defaults := doclibrary.Request{IncludeBBox: &configured}
	for _, tc := range []struct {
		json      string
		wantValue bool
		wantNil   bool
	}{{`false`, false, false}, {`true`, true, false}, {`null`, false, true}} {
		got, _, err := decodeRequest(strings.NewReader(`{"include_bbox":`+tc.json+`}`), defaults)
		if err != nil {
			t.Fatalf("input %s: %v", tc.json, err)
		}
		if tc.wantNil {
			if got.IncludeBBox != nil {
				t.Errorf("input %s should inherit via nil, got %v", tc.json, *got.IncludeBBox)
			}
		} else if got.IncludeBBox == nil || *got.IncludeBBox != tc.wantValue {
			t.Errorf("input %s: include_bbox=%v", tc.json, got.IncludeBBox)
		}
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
