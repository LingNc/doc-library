package doclibrary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const extractionSchema = "compare-paragraph-v4-poppler-layout-v1-unicode-v1"

type Library struct {
	config    Config
	namespace string
}
type sourceFile struct {
	source Source
	doc    string
	size   int64
}

// New validates configuration without scanning documents or checking PDF tools.
// Relative paths are resolved against the process working directory. CLI adapters
// should first resolve config-file paths against that file's directory.
func New(c Config) (*Library, error) {
	if c.Output.Version == 0 {
		c.Output.Version = 1
	}
	if c.Output.Version != 1 && c.Output.Version != 2 {
		return nil, fmt.Errorf("output.version must be 1 or 2")
	}
	if c.Output.IncludeBBox != nil {
		include := *c.Output.IncludeBBox
		c.Output.IncludeBBox = &include
	}
	if c.CacheMode == "" {
		c.CacheMode = "readwrite"
	}
	if c.CacheMode != "readwrite" && c.CacheMode != "off" {
		return nil, fmt.Errorf("cache_mode must be readwrite or off")
	}
	if c.CacheDir == "" {
		base, e := os.UserCacheDir()
		if e != nil {
			return nil, e
		}
		c.CacheDir = filepath.Join(base, "doc-library")
	}
	var err error
	c.CacheDir, err = canonicalFuture(c.CacheDir)
	if err != nil {
		return nil, err
	}
	if c.Search.Match == "" {
		c.Search.Match = "any"
	}
	if c.Search.Match != "any" && c.Search.Match != "all" && c.Search.Match != "phrase" {
		return nil, fmt.Errorf("search.match must be any, all, or phrase")
	}
	if c.Search.Limit < 0 || c.Search.SnippetChars < 0 || c.Search.MaxChars < 0 || c.Limits.MaxFileBytes < 0 || c.Limits.MaxTotalBytes < 0 || c.Limits.MaxFiles < 0 || c.Limits.MaxResults < 0 || c.Limits.ReadLines < 0 || c.Frequency.MaxEntries < 0 || c.Frequency.TextChars < 0 || c.PDF.TimeoutSeconds < 0 {
		return nil, fmt.Errorf("configuration limits cannot be negative")
	}
	if c.Search.Limit == 0 {
		c.Search.Limit = 5
	}
	if c.Search.SnippetChars == 0 {
		c.Search.SnippetChars = 1200
	}
	if c.Search.MaxChars == 0 {
		c.Search.MaxChars = 6000
	}
	if c.Limits.MaxFileBytes == 0 {
		c.Limits.MaxFileBytes = 256 << 20
	}
	if c.Limits.MaxResults == 0 {
		c.Limits.MaxResults = 1000
	}
	if c.Limits.ReadLines == 0 {
		c.Limits.ReadLines = 400
	}
	if c.Frequency.MaxEntries == 0 {
		c.Frequency.MaxEntries = 256
	}
	if c.Frequency.TextChars == 0 {
		c.Frequency.TextChars = 2048
	}
	if c.PDF.TimeoutSeconds == 0 {
		c.PDF.TimeoutSeconds = 120
	}
	if c.PDF.PDFToText == "" {
		c.PDF.PDFToText = "pdftotext"
	}
	if c.PDF.PDFToPPM == "" {
		c.PDF.PDFToPPM = "pdftoppm"
	}
	if c.Search.Limit > c.Limits.MaxResults {
		return nil, fmt.Errorf("search.limit exceeds limits.max_results")
	}
	seen := map[string]bool{}
	c.Sources = append([]Source(nil), c.Sources...)
	for i, s := range c.Sources {
		if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`).MatchString(s.ID) || seen[s.ID] {
			return nil, fmt.Errorf("source id must be unique and contain only letters, digits, _, -, .: %q", s.ID)
		}
		seen[s.ID] = true
		if s.Root == "" {
			return nil, fmt.Errorf("source %s requires root", s.ID)
		}
		s.Root, err = filepath.Abs(s.Root)
		if err != nil {
			return nil, err
		}
		s.Root, err = filepath.EvalSymlinks(s.Root)
		if err != nil {
			return nil, fmt.Errorf("source %s: %w", s.ID, err)
		}
		st, e := os.Stat(s.Root)
		if e != nil || !st.IsDir() {
			return nil, fmt.Errorf("source %s root is not a directory", s.ID)
		}
		if c.CacheMode != "off" && inside(c.CacheDir, s.Root) {
			return nil, fmt.Errorf("cache_dir must not equal or contain source %s root", s.ID)
		}
		for _, p := range append(append([]string{}, s.Include...), s.Exclude...) {
			if _, e := compileGlob(p); e != nil {
				return nil, fmt.Errorf("source %s: %w", s.ID, e)
			}
		}
		s.Include = append([]string(nil), s.Include...)
		s.Exclude = append([]string(nil), s.Exclude...)
		s.Extensions = append([]string(nil), s.Extensions...)
		for j, x := range s.Extensions {
			if x != "*" && !strings.HasPrefix(x, ".") {
				x = "." + x
			}
			s.Extensions[j] = strings.ToLower(x)
		}
		if math.IsNaN(s.Weight) || math.IsInf(s.Weight, 0) || s.Weight < 0 {
			return nil, fmt.Errorf("source %s weight must be finite and nonnegative", s.ID)
		}
		if s.Weight == 0 {
			s.Weight = 1
		}
		c.Sources[i] = s
	}
	// Namespace includes source roots AND filters/settings; no different collection
	// can overwrite another's file or frequency entries in a shared cache root.
	b, _ := json.Marshal(struct {
		Schema string
		Config semanticConfig
	}{extractionSchema, semanticConfig{c.Sources, c.CacheDir, c.CacheMode, c.Search, c.Limits, c.Frequency, c.PDF}})
	return &Library{config: c, namespace: hashBytes(b)}, nil
}

// semanticConfig deliberately preserves the pre-output Config field order and
// tags. Even a zero OutputSettings struct would marshal as {}, changing existing
// namespaces. Presentation settings must never split extraction or frequency.
type semanticConfig struct {
	Sources   []Source          `json:"sources"`
	CacheDir  string            `json:"cache_dir,omitempty"`
	CacheMode string            `json:"cache_mode,omitempty"`
	Search    SearchSettings    `json:"search,omitempty"`
	Limits    Limits            `json:"limits,omitempty"`
	Frequency FrequencySettings `json:"frequency,omitempty"`
	PDF       PDFSettings       `json:"pdf,omitempty"`
}

func hashBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func inside(root, path string) bool {
	r, e := filepath.Rel(root, path)
	return e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

// Resolve symlinks even when the final directory does not yet exist.
func canonicalFuture(path string) (string, error) {
	a, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	var suffix []string
	for {
		real, err := filepath.EvalSymlinks(a)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				real = filepath.Join(real, suffix[i])
			}
			return real, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(a)
		if parent == a {
			return "", err
		}
		suffix = append(suffix, filepath.Base(a))
		a = parent
	}
}
func cleanDoc(name string) (string, error) {
	if name == "" || name == "." {
		return ".", nil
	}
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") || filepath.IsAbs(name) || filepath.VolumeName(name) != "" || (len(name) > 1 && name[1] == ':') {
		return "", fmt.Errorf("document path must be relative")
	}
	name = strings.ReplaceAll(name, "\\", "/")
	for _, s := range strings.Split(name, "/") {
		if s == ".." {
			return "", fmt.Errorf("document path must not contain '..'")
		}
	}
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(name))), nil
}

// compileGlob supports *, ?, [] using path.Match syntax and ** for recursive
// directories. Patterns without '/' match a basename at any depth.
func compileGlob(pattern string) (*regexp.Regexp, error) {
	pattern = strings.ReplaceAll(pattern, "\\", "/")
	if pattern == "" || strings.HasPrefix(pattern, "/") {
		return nil, fmt.Errorf("invalid relative glob %q", pattern)
	}
	for _, part := range strings.Split(pattern, "/") {
		if part == ".." {
			return nil, fmt.Errorf("invalid parent segment in glob %q", pattern)
		}
	}
	var b strings.Builder
	b.WriteString("^")
	if !strings.Contains(pattern, "/") {
		b.WriteString("(?:.*/)?")
	}
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '[':
			j := i + 1
			for j < len(pattern) && pattern[j] != ']' {
				j++
			}
			if j == len(pattern) {
				return nil, fmt.Errorf("invalid glob %q", pattern)
			}
			part := pattern[i : j+1]
			if strings.HasPrefix(part, "[!") {
				part = "[^" + part[2:]
			}
			b.WriteString(part)
			i = j
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	b.WriteString("$")
	re, e := regexp.Compile(b.String())
	if e != nil {
		return nil, fmt.Errorf("invalid glob %q: %w", pattern, e)
	}
	return re, nil
}
func matchesGlob(pattern, doc string) bool {
	re, e := compileGlob(pattern)
	return e == nil && re.MatchString(doc)
}
func allowed(s Source, doc string) bool {
	included := len(s.Include) == 0
	for _, p := range s.Include {
		if matchesGlob(p, doc) {
			included = true
			break
		}
	}
	if !included {
		return false
	}
	for _, p := range s.Exclude {
		if matchesGlob(p, doc) || matchesGlob(strings.TrimSuffix(p, "/")+"/**", doc) {
			return false
		}
	}
	if len(s.Extensions) > 0 {
		ext := strings.ToLower(filepath.Ext(doc))
		ok := false
		for _, x := range s.Extensions {
			if x == "*" || x == ext {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}
func (l *Library) selected(r Request) ([]Source, error) {
	if r.Source != "" && len(r.Sources) > 0 {
		return nil, fmt.Errorf("source and sources are mutually exclusive")
	}
	ids := r.Sources
	if r.Source != "" {
		ids = []string{r.Source}
	}
	if len(ids) == 0 {
		return l.config.Sources, nil
	}
	var out []Source
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return nil, fmt.Errorf("duplicate source selector: %s", id)
		}
		seen[id] = true
		found := false
		for _, s := range l.config.Sources {
			if s.ID == id {
				out = append(out, s)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown source: %s", id)
		}
	}
	return out, nil
}
func (l *Library) scan(ctx context.Context, sources []Source, scope string) ([]sourceFile, error) {
	scope, e := cleanDoc(scope)
	if e != nil {
		return nil, e
	}
	var files []sourceFile
	var total int64
	for _, s := range sources {
		e = filepath.WalkDir(s.Root, func(p string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if l.reservedPath(p) {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				rel, err := filepath.Rel(s.Root, p)
				if err != nil {
					return err
				}
				rel = filepath.ToSlash(rel)
				if rel != "." {
					for _, pattern := range s.Exclude {
						base := strings.TrimSuffix(strings.TrimSuffix(pattern, "/"), "/**")
						if matchesGlob(base, rel) {
							return fs.SkipDir
						}
					}
				}
				return nil
			}
			if !entry.Type().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(s.Root, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if scope != "." && rel != scope && !strings.HasPrefix(rel, strings.TrimSuffix(scope, "/")+"/") {
				return nil
			}
			if !allowed(s, rel) {
				return nil
			}
			st, err := entry.Info()
			if err != nil {
				return err
			}
			if st.Size() > l.config.Limits.MaxFileBytes {
				return fmt.Errorf("source %s file %s exceeds max_file_bytes (%d)", s.ID, rel, l.config.Limits.MaxFileBytes)
			}
			total += st.Size()
			files = append(files, sourceFile{s, rel, st.Size()})
			if l.config.Limits.MaxFiles > 0 && len(files) > l.config.Limits.MaxFiles {
				return fmt.Errorf("scan exceeds max_files; narrow source/doc scope or raise configured quota")
			}
			if l.config.Limits.MaxTotalBytes > 0 && total > l.config.Limits.MaxTotalBytes {
				return fmt.Errorf("scan exceeds max_total_bytes; narrow scope or raise configured quota")
			}
			return nil
		})
		if e != nil {
			return nil, e
		}
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].source.ID != files[j].source.ID {
			return files[i].source.ID < files[j].source.ID
		}
		return files[i].doc < files[j].doc
	})
	return files, nil
}

// readFile uses Go's rooted filesystem API so a concurrent symlink replacement
// cannot turn a checked relative document into an outside-root file access.
func (l *Library) readFile(s Source, doc string) ([]byte, error) {
	doc, e := cleanDoc(doc)
	if e != nil {
		return nil, e
	}
	if !allowed(s, doc) || l.reservedPath(filepath.Join(s.Root, filepath.FromSlash(doc))) {
		return nil, fmt.Errorf("document excluded by source %s policy: %s", s.ID, doc)
	}
	root, e := os.OpenRoot(s.Root)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	f, e := root.Open(filepath.FromSlash(doc))
	if e != nil {
		return nil, fmt.Errorf("source %s document %s: %w", s.ID, doc, e)
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("document is not a regular file: %s", doc)
	}
	if st.Size() > l.config.Limits.MaxFileBytes {
		return nil, fmt.Errorf("document exceeds max_file_bytes: %s", doc)
	}
	b, e := io.ReadAll(io.LimitReader(f, l.config.Limits.MaxFileBytes+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > l.config.Limits.MaxFileBytes {
		return nil, fmt.Errorf("document exceeds max_file_bytes: %s", doc)
	}
	return b, nil
}
func isText(data []byte) bool { return utf8.Valid(data) && !strings.ContainsRune(string(data), 0) }
func (l *Library) cachePath(parts ...string) string {
	return filepath.Join(append([]string{l.config.CacheDir, l.namespace}, parts...)...)
}
func (l *Library) reservedPath(path string) bool {
	if l.config.CacheMode != "off" && inside(l.config.CacheDir, path) {
		return true
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.HasPrefix(part, ".doc-library-preview-") {
			return true
		}
	}
	return false
}
func (l *Library) outputDir(requested string) (string, error) {
	if requested == "" {
		requested = l.cachePath("预览")
	}
	p, e := canonicalFuture(requested)
	if e != nil {
		return "", e
	}
	for _, s := range l.config.Sources {
		if p == s.Root || inside(p, s.Root) {
			return "", fmt.Errorf("view output must not equal or contain source %s root", s.ID)
		}
	}
	if e = os.MkdirAll(p, 0700); e != nil {
		return "", e
	}
	return os.MkdirTemp(p, ".doc-library-preview-")
}
func atomicJSON(path string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".doc-library-*.tmp")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return os.Rename(name, path)
}
func acquireLock(ctx context.Context, path string) (func(), error) {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	for {
		if err := ctx.Err(); err != nil {
			f.Close()
			return nil, err
		}
		locked, err := tryFileLock(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		if locked {
			return func() { _ = f.Close() }, nil
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, fmt.Errorf("cache lock %s: %w", path, ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func safeError(err error) bool { return errors.Is(err, os.ErrNotExist) }
