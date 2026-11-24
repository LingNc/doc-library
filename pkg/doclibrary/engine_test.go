package doclibrary

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func coreFixture(t *testing.T, files map[string]string) (*Library, string, string) {
	t.Helper()
	root := t.TempDir()
	for name, text := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cache := t.TempDir()
	l, e := New(Config{Sources: []Source{{ID: "a", Root: root}}, CacheDir: cache})
	if e != nil {
		t.Fatal(e)
	}
	return l, root, cache
}
func coreExecute(t *testing.T, l *Library, r Request) Response {
	t.Helper()
	resp, e := l.Execute(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	if !resp.OK {
		t.Fatal("successful request did not set ok")
	}
	return resp
}
func TestCoreUnicodeMixedAndPhrase(t *testing.T) {
	l, _, _ := coreFixture(t, map[string]string{"mixed.txt": "中文 LuaDraw 混合文字\n\nLuaDraw only\n\n中 文 连 续\n", "latin.txt": "FranÇais API_42\n"})
	r := coreExecute(t, l, Request{Query: "中文 LuaDraw", Match: "all"})
	if len(r.Results) != 1 || !strings.Contains(r.Results[0].Text, "中文") {
		t.Fatalf("mixed query lost Chinese token: %#v", r.Results)
	}
	r = coreExecute(t, l, Request{Query: "中文连续", Match: "phrase"})
	if len(r.Results) != 1 {
		t.Fatalf("Chinese layout spaces broke phrase: %#v", r.Results)
	}
	r = coreExecute(t, l, Request{Query: "français api_42", Match: "all"})
	if len(r.Results) != 1 {
		t.Fatalf("Unicode case folding failed: %#v", r.Results)
	}
}
func TestCoreContentCacheSameMetadataAndPerFileReuse(t *testing.T) {
	l, root, _ := coreFixture(t, map[string]string{"a.txt": "alpha text\n", "b.txt": "bravo text\n"})
	first := coreExecute(t, l, Request{Mode: "index"})
	if first.Cache.Misses != 2 {
		t.Fatalf("cold cache: %+v", first.Cache)
	}
	warm := coreExecute(t, l, Request{Mode: "index"})
	if warm.Cache.Hits != 2 || warm.Cache.Misses != 0 {
		t.Fatalf("warm cache: %+v", warm.Cache)
	}
	p := filepath.Join(root, "a.txt")
	st, e := os.Stat(p)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(p, []byte("omega text\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Chtimes(p, st.ModTime(), st.ModTime()); e != nil {
		t.Fatal(e)
	}
	changed := coreExecute(t, l, Request{Mode: "index"})
	if changed.Cache.Hits != 1 || changed.Cache.Misses != 1 || changed.Cache.Fingerprint == first.Cache.Fingerprint {
		t.Fatalf("same-metadata content change not detected: %+v", changed.Cache)
	}
}
func TestCorePureTextDoesNotProbePDF(t *testing.T) {
	l, _, _ := coreFixture(t, map[string]string{"ok.txt": "needle 中文\n", "unavailable.pdf": "not a PDF\n"})
	l.config.PDF.PDFToText = "/no-such-pdftotext"
	coreExecute(t, l, Request{Mode: "list"})
	coreExecute(t, l, Request{Mode: "read", Doc: "ok.txt"})
	coreExecute(t, l, Request{Mode: "grep", Query: "needle"})
	r := coreExecute(t, l, Request{Query: "needle", Kinds: []string{"source"}})
	if len(r.Results) != 1 {
		t.Fatal(r)
	}
	if _, e := l.Execute(context.Background(), Request{Mode: "read", Doc: "unavailable.pdf"}); e == nil || !strings.Contains(e.Error(), "required PDF executable") {
		t.Fatalf("missing executable not reported lazily: %v", e)
	}
}
func TestCorePolicyAndRootBoundary(t *testing.T) {
	_, root, cache := coreFixture(t, map[string]string{"public/a.txt": "safe", "private/hidden.txt": "secret"})
	l, e := New(Config{Sources: []Source{{ID: "s", Root: root, Include: []string{"public/**/*.txt"}}}, CacheDir: cache})
	if e != nil {
		t.Fatal(e)
	}
	for _, doc := range []string{"private/hidden.txt", "../outside.txt", "/etc/passwd", "C:\\secret.txt"} {
		if _, e = l.Execute(context.Background(), Request{Mode: "read", Doc: doc}); e == nil {
			t.Fatalf("unsafe read accepted: %q", doc)
		}
	}
	coreExecute(t, l, Request{Mode: "read", Doc: "public/a.txt"})
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if e = os.WriteFile(outside, []byte("secret"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(outside, filepath.Join(root, "public", "escape.txt")); e == nil {
		if _, e = l.Execute(context.Background(), Request{Mode: "read", Doc: "public/escape.txt"}); e == nil {
			t.Fatal("symlink escaped configured root")
		}
	}
}
func TestCoreCacheInsideRootExcluded(t *testing.T) {
	root := t.TempDir()
	if e := os.WriteFile(filepath.Join(root, "doc.txt"), []byte("needle"), 0600); e != nil {
		t.Fatal(e)
	}
	l, e := New(Config{Sources: []Source{{ID: "s", Root: root}}, CacheDir: filepath.Join(root, "build", "cache")})
	if e != nil {
		t.Fatal(e)
	}
	coreExecute(t, l, Request{Query: "needle"})
	r := coreExecute(t, l, Request{Mode: "status"})
	if len(r.Results) != 1 || r.Results[0].Doc != "doc.txt" {
		t.Fatalf("generated cache entered source scan: %#v", r.Results)
	}
}
func TestCoreCanonicalFrequencyConcurrentPassiveAndInvalidation(t *testing.T) {
	l, root, cache := coreFixture(t, map[string]string{"one.txt": "needle line one\nneedle line two\n\nsecond\n", "two.txt": "other paragraph\n"})
	coreExecute(t, l, Request{Mode: "read", Doc: "two.txt"})
	const workers = 6
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other, e := New(Config{Sources: []Source{{ID: "a", Root: root}}, CacheDir: cache})
			if e == nil {
				_, e = other.Execute(context.Background(), Request{Mode: "read", Doc: "one.txt", StartLine: 2, EndLine: 2})
			}
			if e != nil {
				errs <- e
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	r := coreExecute(t, l, Request{Mode: "frequent", Passive: true})
	if len(r.Results) != 2 || r.Results[0].Hits != workers || r.Results[0].StartLine != 1 || r.Results[0].EndLine != 2 {
		t.Fatalf("canonical counts lost: %#v", r.Results)
	}
	before, e := os.ReadFile(l.cachePath("frequency-v1.json"))
	if e != nil {
		t.Fatal(e)
	}
	coreExecute(t, l, Request{Mode: "frequent", Passive: true})
	after, _ := os.ReadFile(l.cachePath("frequency-v1.json"))
	if string(before) != string(after) {
		t.Fatal("passive frequent wrote history")
	}
	if e = os.WriteFile(filepath.Join(root, "one.txt"), []byte("new content\n"), 0600); e != nil {
		t.Fatal(e)
	}
	r = coreExecute(t, l, Request{Mode: "frequent", Passive: true})
	if len(r.Results) != 1 || r.Results[0].Doc != "two.txt" {
		t.Fatalf("invalidation removed unaffected document or retained stale text: %#v", r.Results)
	}
}
func TestCorePDFLayoutKeepsSameHeightColumns(t *testing.T) {
	xml := `<doc><page width="500" height="700"><flow><block><line><word xMin="10" yMin="10" xMax="20" yMax="20">中</word><word xMin="20" yMin="10" xMax="30" yMax="20">文</word></line></block></flow><flow><block><line><word xMin="300" yMin="10" xMax="350" yMax="20">right</word></line></block></flow></page></doc>`
	pages, e := parsePDFXML(strings.NewReader(xml))
	if e != nil {
		t.Fatal(e)
	}
	if len(pages) != 1 || len(pages[0].Blocks) != 2 || pages[0].Blocks[0].Text != "中文" || pages[0].Blocks[1].Text != "right" {
		t.Fatalf("two columns collapsed or CJK split: %#v", pages)
	}
}
func TestCoreChineseSeparateTerms(t *testing.T) {
	l, _, _ := coreFixture(t, map[string]string{"both.txt": "导数的计算涉及换元步骤\n", "one.txt": "导数运算\n"})
	r := coreExecute(t, l, Request{Query: "导数 换元", Match: "all"})
	if len(r.Results) != 1 || r.Results[0].Doc != "both.txt" {
		t.Fatalf("query whitespace between Chinese terms was lost: %#v", r.Results)
	}
}
func TestCoreSchemaWithoutSources(t *testing.T) {
	l, e := New(Config{})
	if e != nil {
		t.Fatal(e)
	}
	r := coreExecute(t, l, Request{Mode: "schema"})
	if r.Schema["request"] == nil || r.Schema["config"] == nil {
		t.Fatal("missing machine-readable schema")
	}
}
