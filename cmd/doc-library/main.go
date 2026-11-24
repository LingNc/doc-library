package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	doclibrary "doc-library"
	"gopkg.in/yaml.v3"
)

const maxJSONLine = 4 << 20

func moveLeadingMode(args []string) []string {
	if len(args) == 0 {
		return args
	}
	switch args[0] {
	case "search", "read", "view", "list", "find", "grep", "frequent", "index", "status", "schema":
		return append(append([]string(nil), args[1:]...), args[0])
	default:
		return args
	}
}

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	args = moveLeadingMode(args)
	fs := flag.NewFlagSet("doc-library", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var configPath, cacheDir, requestPath, mode, query, doc, match, region, outDir string
	var requestStdin, jsonl, regex, caseSensitive, version bool
	var roots rootList
	var sources, kinds, extensions stringList
	var limit, offset, snippet, maxChars, lineChars, startLine, endLine, page, dpi, zoom int
	var recursive, includeImage, inlineImages, passive bool
	var entryID string
	var timeout time.Duration
	fs.StringVar(&configPath, "config", "", "JSON or YAML configuration file")
	fs.Var(&roots, "root", "document root id=path (repeatable; id may be omitted)")
	fs.StringVar(&cacheDir, "cache-dir", "", "cache directory")
	fs.StringVar(&mode, "mode", "", "request mode: search/read/view/list/find/grep/frequent/index/status/schema")
	fs.StringVar(&query, "query", "", "search query")
	fs.Var(&sources, "source", "source id filter (repeatable)")
	fs.Var(&kinds, "kind", "document kind filter: source or pdf (repeatable)")
	fs.Var(&extensions, "ext", "file extension filter (repeatable; alias: --extension)")
	fs.Var(&extensions, "extension", "file extension filter (repeatable; alias: --ext)")
	fs.StringVar(&doc, "doc", "", "document path")
	fs.IntVar(&limit, "limit", 0, "result limit")
	fs.IntVar(&offset, "offset", 0, "result offset")
	fs.IntVar(&snippet, "snippet-chars", 0, "snippet character budget")
	fs.IntVar(&maxChars, "max-chars", 0, "maximum result text characters (not total JSON size)")
	fs.IntVar(&lineChars, "line-chars", 0, "maximum characters per line")
	fs.Func("match", "match mode: any, all, or phrase", func(v string) error {
		if v != "any" && v != "all" && v != "phrase" {
			return errors.New("must be any, all, or phrase")
		}
		match = v
		return nil
	})
	fs.BoolVar(&regex, "regex", false, "treat query as regular expression")
	fs.BoolVar(&caseSensitive, "case-sensitive", false, "case-sensitive matching")
	fs.BoolVar(&recursive, "recursive", false, "include nested documents")
	fs.BoolVar(&includeImage, "include-image", false, "include a rendered image")
	fs.BoolVar(&inlineImages, "inline-images", false, "inline image data in the response")
	fs.BoolVar(&passive, "passive", false, "avoid recording frequency usage")
	fs.StringVar(&entryID, "entry-id", "", "frequency entry identifier")
	fs.IntVar(&startLine, "start-line", 0, "first text line")
	fs.IntVar(&endLine, "end-line", 0, "last text line")
	fs.IntVar(&page, "page", 0, "PDF physical page")
	fs.IntVar(&dpi, "dpi", 0, "PDF render DPI")
	fs.IntVar(&zoom, "zoom", 0, "PDF preview zoom")
	fs.StringVar(&region, "region", "", "PDF region left,top,right,bottom (percentages)")
	fs.StringVar(&outDir, "out", "", "output directory")
	fs.DurationVar(&timeout, "timeout", 0, "request timeout")
	fs.BoolVar(&requestStdin, "stdin", false, "read one JSON request from stdin")
	fs.StringVar(&requestPath, "request", "", "read one JSON request from file")
	fs.BoolVar(&jsonl, "jsonl", false, "read one JSON request per stdin line")
	fs.BoolVar(&version, "version", false, "print version as JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		writeError(stdout, err, "")
		return 2
	}
	if version {
		return writeJSON(stdout, map[string]any{"version": doclibrary.Version, "ok": true})
	}
	if fs.NArg() > 0 {
		if fs.NArg() != 1 || mode != "" {
			writeError(stdout, errors.New("provide one mode or --mode, not both"), "")
			return 2
		}
		mode = fs.Arg(0)
	}
	if (requestPath != "" && (requestStdin || jsonl)) || (requestStdin && jsonl) {
		writeError(stdout, errors.New("--request, --stdin, and --jsonl are mutually exclusive"), "")
		return 2
	}
	for _, kind := range kinds {
		if kind != "source" && kind != "pdf" {
			return fail(stdout, fmt.Errorf("--kind must be source or pdf, got %q", kind))
		}
	}
	if timeout < 0 {
		return fail(stdout, errors.New("--timeout cannot be negative"))
	}

	cfg := doclibrary.Config{}
	configDir := ""
	if configPath != "" {
		b, err := os.ReadFile(configPath)
		if err != nil {
			return fail(stdout, err)
		}
		switch strings.ToLower(filepath.Ext(configPath)) {
		case ".yaml", ".yml":
			dec := yaml.NewDecoder(bytes.NewReader(b))
			dec.KnownFields(true)
			if err = dec.Decode(&cfg); err == nil {
				var extra any
				if e := dec.Decode(&extra); e != io.EOF {
					if e == nil {
						err = errors.New("multiple YAML documents")
					} else {
						err = e
					}
				}
			}
		default:
			err = strictJSON(b, &cfg)
		}
		if err != nil {
			return fail(stdout, fmt.Errorf("config: %w", err))
		}
		abs, err := filepath.Abs(configPath)
		if err != nil {
			return fail(stdout, err)
		}
		configDir = filepath.Dir(abs)
		resolveConfigPaths(&cfg, configDir)
	}
	for i, item := range roots {
		id, path, hasID := strings.Cut(item, "=")
		if !hasID {
			id, path = fmt.Sprintf("root%d", i+1), item
		}
		if strings.TrimSpace(path) == "" {
			return fail(stdout, fmt.Errorf("--root %q has an empty path", item))
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return fail(stdout, err)
		}
		cfg.Sources = append(cfg.Sources, doclibrary.Source{ID: id, Root: abs})
	}
	for i := range cfg.Sources {
		if cfg.Sources[i].ID == "" {
			cfg.Sources[i].ID = fmt.Sprintf("source%d", i+1)
		}
	}
	if cacheDir != "" {
		cfg.CacheDir = absoluteFrom(cacheDir, "")
	}
	if cfg.CacheDir == "" {
		if env := os.Getenv("DOC_LIBRARY_CACHE"); env != "" {
			cfg.CacheDir = absoluteFrom(env, "")
		} else if base, err := os.UserCacheDir(); err == nil {
			cfg.CacheDir = filepath.Join(base, "doc-library")
		}
	}
	if cfg.CacheDir != "" && !filepath.IsAbs(cfg.CacheDir) {
		cfg.CacheDir = absoluteFrom(cfg.CacheDir, configDir)
	}
	if mode == "" && requestPath == "" && !requestStdin && !jsonl {
		writeError(stdout, errors.New("mode is required (or use --request/--stdin/--jsonl)"), "")
		fs.Usage()
		return 2
	}
	lib, err := doclibrary.New(cfg)
	if err != nil {
		return fail(stdout, err)
	}
	parsedRegion, err := parseRegion(region)
	if err != nil {
		return fail(stdout, err)
	}
	base := buildRequest(mode, query, sources, kinds, extensions, doc, limit, offset, snippet, maxChars, lineChars, match, regex, caseSensitive, recursive, includeImage, inlineImages, passive, entryID, startLine, endLine, page, dpi, zoom, outDir)
	base.Region = parsedRegion
	if jsonl {
		return runJSONL(lib, stdin, stdout, base, timeout)
	}
	var input io.Reader
	if requestPath != "" {
		f, err := os.Open(requestPath)
		if err != nil {
			return fail(stdout, err)
		}
		defer f.Close()
		input = f
	} else if requestStdin {
		input = stdin
	}
	requestID := ""
	if input != nil {
		r, id, err := decodeRequest(input, base)
		if err != nil {
			writeError(stdout, err, "")
			return 2
		}
		base, requestID = r, id
	}
	resp, err := execute(lib, base, timeout)
	if err != nil {
		writeError(stdout, err, requestID)
		return 2
	}
	if requestID != "" {
		return writeJSON(stdout, withRequestID(resp, requestID))
	}
	return writeJSON(stdout, resp)
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

type rootList []string

func (s *rootList) String() string     { return strings.Join(*s, ",") }
func (s *rootList) Set(v string) error { *s = append(*s, v); return nil }

func buildRequest(mode, query string, sources, kinds, extensions []string, doc string, limit, offset, snippet, maxChars, lineChars int, match string, regex, caseSensitive, recursive, includeImage, inlineImages, passive bool, entryID string, startLine, endLine, page, dpi, zoom int, outDir string) doclibrary.Request {
	return doclibrary.Request{Mode: mode, Query: query, Sources: append([]string(nil), sources...), Kinds: append([]string(nil), kinds...), Extensions: append([]string(nil), extensions...), Doc: doc, Limit: limit, Offset: offset, SnippetChars: snippet, MaxChars: maxChars, LineChars: lineChars, Match: match, Regex: regex, CaseSensitive: caseSensitive, Recursive: recursive, IncludeImage: includeImage, InlineImages: inlineImages, Passive: passive, EntryID: entryID, StartLine: startLine, EndLine: endLine, Page: page, DPI: dpi, Zoom: zoom, OutDir: outDir}
}

func parseRegion(value string) (*doclibrary.Region, error) {
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	if len(parts) != 4 {
		return nil, errors.New("--region requires left,top,right,bottom")
	}
	var v [4]float64
	for i := range parts {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(parts[i]), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid --region coordinate %q", parts[i])
		}
		v[i] = parsed
	}
	return &doclibrary.Region{Left: v[0], Top: v[1], Right: v[2], Bottom: v[3]}, nil
}

func decodeRequest(r io.Reader, defaults doclibrary.Request) (doclibrary.Request, string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxJSONLine+1))
	if err != nil {
		return defaults, "", err
	}
	if len(data) > maxJSONLine {
		return defaults, "", errors.New("request exceeds 4 MiB")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return defaults, "", err
	}
	if raw == nil {
		return defaults, "", errors.New("request must be a JSON object")
	}
	id, requestRaw := "", data
	if a, ok := raw["arguments"]; ok {
		for k := range raw {
			if k != "arguments" && k != "request_id" {
				return defaults, "", fmt.Errorf("unknown envelope field %q", k)
			}
		}
		if v, ok := raw["request_id"]; ok {
			if err := json.Unmarshal(v, &id); err != nil {
				return defaults, "", errors.New("request_id must be a string")
			}
		}
		requestRaw = a
	}
	var patch map[string]json.RawMessage
	if err := json.Unmarshal(requestRaw, &patch); err != nil {
		return defaults, "", err
	}
	if patch == nil {
		return defaults, "", errors.New("request arguments must be a JSON object")
	}
	_, hasSource := patch["source"]
	_, hasSources := patch["sources"]
	if hasSource && hasSources {
		return defaults, "", errors.New("source and sources are mutually exclusive")
	}
	base, _ := json.Marshal(defaults)
	var merged map[string]json.RawMessage
	_ = json.Unmarshal(base, &merged)
	if hasSource {
		delete(merged, "sources")
	}
	if hasSources {
		delete(merged, "source")
	}
	for k, v := range patch {
		merged[k] = v
	}
	joined, _ := json.Marshal(merged)
	var out doclibrary.Request
	if err := strictJSON(joined, &out); err != nil {
		return defaults, "", err
	}
	if out.Source != "" && len(out.Sources) > 0 {
		return defaults, "", errors.New("source and sources are mutually exclusive")
	}
	return out, id, nil
}

func runJSONL(lib *doclibrary.Library, input io.Reader, output io.Writer, defaults doclibrary.Request, timeout time.Duration) int {
	reader := bufio.NewReaderSize(input, 64*1024)
	bad := false
	for {
		line, lineErr, done := readJSONLine(reader)
		if len(bytes.TrimSpace(line)) > 0 || lineErr != nil {
			if lineErr != nil {
				writeError(output, lineErr, "")
				bad = true
			} else {
				r, id, err := decodeRequest(bytes.NewReader(line), defaults)
				if err == nil {
					var resp doclibrary.Response
					resp, err = execute(lib, r, timeout)
					if err == nil {
						var value any = resp
						if id != "" {
							value = withRequestID(resp, id)
						}
						if writeJSON(output, value) != 0 {
							bad = true
						}
						if done {
							break
						}
						continue
					}
				}
				bad = true
				writeError(output, err, id)
			}
		}
		if done {
			break
		}
	}
	if bad {
		return 1
	}
	return 0
}

func readJSONLine(reader *bufio.Reader) ([]byte, error, bool) {
	var line []byte
	oversized := false
	for {
		part, err := reader.ReadSlice('\n')
		if !oversized {
			if len(line)+len(part) > maxJSONLine {
				oversized = true
			} else {
				line = append(line, part...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil && err != io.EOF {
			return nil, err, true
		}
		if oversized {
			return nil, errors.New("JSONL record exceeds 4 MiB"), err == io.EOF
		}
		return line, nil, err == io.EOF
	}
}

func execute(lib *doclibrary.Library, req doclibrary.Request, timeout time.Duration) (doclibrary.Response, error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return lib.Execute(ctx, req)
}
func strictJSON(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
func resolveConfigPaths(cfg *doclibrary.Config, base string) {
	for i := range cfg.Sources {
		cfg.Sources[i].Root = absoluteFrom(cfg.Sources[i].Root, base)
	}
	if cfg.CacheDir != "" {
		cfg.CacheDir = absoluteFrom(cfg.CacheDir, base)
	}
}
func absoluteFrom(path, base string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	if base == "" {
		base, _ = os.Getwd()
	}
	return filepath.Clean(filepath.Join(base, path))
}
func writeJSON(w io.Writer, value any) int {
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return 2
	}
	return 0
}
func writeError(w io.Writer, err error, id string) {
	v := map[string]any{"version": doclibrary.Version, "ok": false, "error": map[string]string{"code": "invalid_request", "message": err.Error()}}
	if id != "" {
		v["request_id"] = id
	}
	_ = writeJSON(w, v)
}
func withRequestID(resp doclibrary.Response, id string) map[string]any {
	b, _ := json.Marshal(resp)
	var v map[string]any
	_ = json.Unmarshal(b, &v)
	v["request_id"] = id
	return v
}
func fail(w io.Writer, err error) int { writeError(w, err, ""); return 2 }
