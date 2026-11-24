package doclibrary

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newFixtureLibrary(t *testing.T, sources []Source) *Library {
	t.Helper()
	cfg := Config{
		Sources:   sources,
		CacheDir:  filepath.Join(t.TempDir(), "cache"),
		Frequency: FrequencySettings{MaxEntries: 64, TextChars: 2048},
	}
	lib, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return lib
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func putDoc(t *testing.T, root, rel, content string) string {
	t.Helper()
	name := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return name
}

func requireOK(t *testing.T, got Response, err error) Response {
	t.Helper()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !got.OK {
		t.Fatalf("response not OK: mode=%q text=%q warnings=%v", got.Mode, got.Text, got.Warnings)
	}
	return got
}

func mustExecute(t *testing.T, lib *Library, req Request) Response {
	t.Helper()
	got, err := lib.Execute(context.Background(), req)
	return requireOK(t, got, err)
}

func resultContaining(t *testing.T, got Response, needle string) Result {
	t.Helper()
	for _, result := range got.Results {
		if strings.Contains(result.Text, needle) {
			return result
		}
	}
	t.Fatalf("no result contains %q: %#v", needle, got.Results)
	return Result{}
}

func TestSourceLocalCacheIsAutomaticallyExcluded(t *testing.T) {
	root := fixtureRoot(t)
	putDoc(t, root, "visible.md", "visible-document-token")
	cache := filepath.Join(root, ".doc-library-cache")
	lib, err := New(Config{
		Sources:  []Source{{ID: "local", Root: root, Extensions: []string{".md", ".json"}}},
		CacheDir: cache,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := mustExecute(t, lib, Request{Mode: "list", Recursive: true, Limit: 20})
	if len(got.Results) != 1 || got.Results[0].Doc != "visible.md" {
		t.Fatalf("source-local cache leaked into listing: %#v", got.Results)
	}
	searched := mustExecute(t, lib, Request{Mode: "search", Query: "visible-document-token", Limit: 20})
	resultContaining(t, searched, "visible-document-token")
	for _, r := range searched.Results {
		if strings.Contains(r.Doc, ".doc-library-cache") {
			t.Fatalf("cache path became searchable: %#v", r)
		}
	}
}

func TestTextModesDoNotRequireWorkingPoppler(t *testing.T) {
	root := fixtureRoot(t)
	putDoc(t, root, "plain.md", "plain text marker in a text-only source")
	lib, err := New(Config{
		Sources:  []Source{{ID: "text", Root: root, Extensions: []string{".md"}}},
		CacheDir: filepath.Join(t.TempDir(), "cache"),
		PDF:      PDFSettings{PDFToText: filepath.Join(t.TempDir(), "missing-pdftotext")},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []Request{
		{Mode: "read", Source: "text", Doc: "plain.md", StartLine: 1, EndLine: 2},
		{Mode: "list", Source: "text", Recursive: true},
		{Mode: "find", Source: "text", Query: "*.md"},
		{Mode: "grep", Source: "text", Query: "plain text marker"},
		{Mode: "search", Source: "text", Query: "plain text marker"},
	} {
		got := mustExecute(t, lib, req)
		if req.Mode == "read" {
			if !strings.Contains(got.Text, "plain text marker") {
				t.Fatalf("read lost text: %q", got.Text)
			}
			continue
		}
		if len(got.Results) == 0 {
			t.Fatalf("%s unexpectedly required Poppler or returned no match", req.Mode)
		}
	}
}

func TestDuplicateBasenamesRemainSourceQualified(t *testing.T) {
	rootA, rootB := fixtureRoot(t), fixtureRoot(t)
	putDoc(t, rootA, "manual/guide.md", "first-source-only phrase")
	putDoc(t, rootB, "reference/guide.md", "second-source-only phrase")
	lib := newFixtureLibrary(t, []Source{
		{ID: "yantrace", Root: rootA, Extensions: []string{".md"}},
		{ID: "luadraw", Root: rootB, Extensions: []string{".md"}},
	})

	got := mustExecute(t, lib, Request{Mode: "search", Query: "second-source-only", Limit: 10})
	r := resultContaining(t, got, "second-source-only")
	if r.Source != "luadraw" || !strings.HasSuffix(filepath.ToSlash(r.Doc), "reference/guide.md") {
		t.Fatalf("same basename lost source/doc qualification: %#v", r)
	}
	both := mustExecute(t, lib, Request{Mode: "search", Query: "phrase", Limit: 10})
	if resultContaining(t, both, "first-source-only").Source != "yantrace" || resultContaining(t, both, "second-source-only").Source != "luadraw" {
		t.Fatalf("duplicate basenames collided across sources: %#v", both.Results)
	}
}

func TestSourceAndIncludeExcludeApplyAcrossModes(t *testing.T) {
	rootA, rootB := fixtureRoot(t), fixtureRoot(t)
	putDoc(t, rootA, "included/target.md", "needle allowed in source A")
	putDoc(t, rootA, "private/secret.md", "needle must never appear secret")
	putDoc(t, rootA, "ignored.txt", "needle has disallowed extension")
	putDoc(t, rootB, "included/target.md", "needle belongs to source B")
	lib := newFixtureLibrary(t, []Source{
		{ID: "A", Root: rootA, Include: []string{"included/**", "private/**"}, Exclude: []string{"private/**"}, Extensions: []string{".md"}},
		{ID: "B", Root: rootB, Include: []string{"included/**"}, Extensions: []string{".md"}},
	})
	for _, tc := range []struct {
		name string
		req  Request
	}{
		{"search", Request{Mode: "search", Query: "needle", Source: "A", Limit: 20}},
		{"grep", Request{Mode: "grep", Query: "needle", Source: "A", Limit: 20}},
		{"find", Request{Mode: "find", Query: "**/*.md", Source: "A", Limit: 20}},
		{"list", Request{Mode: "list", Source: "A", Recursive: true, Limit: 20}},
		{"read", Request{Mode: "read", Source: "A", Doc: "included/target.md", StartLine: 1, EndLine: 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mustExecute(t, lib, tc.req)
			if tc.name == "read" {
				if !strings.Contains(got.Text, "needle allowed in source A") || strings.Contains(got.Text, "source B") {
					t.Fatalf("read ignored source filter: %q", got.Text)
				}
				return
			}
			if len(got.Results) == 0 {
				t.Fatalf("%s returned no expected entries", tc.name)
			}
			for _, r := range got.Results {
				if r.Source != "A" || strings.Contains(r.Doc, "secret") || strings.Contains(r.Doc, "ignored.txt") || strings.Contains(r.Text, "must never appear") {
					t.Fatalf("%s escaped source/include/exclude/extension filters: %#v", tc.name, r)
				}
			}
		})
	}
	for _, mode := range []string{"search", "grep", "find", "list"} {
		got, err := lib.Execute(context.Background(), Request{Mode: mode, Query: "needle", Source: "missing", Recursive: true, Limit: 20})
		if err == nil && len(got.Results) != 0 {
			t.Fatalf("%s ignored unknown source filter: %#v", mode, got.Results)
		}
	}
	secret, err := lib.Execute(context.Background(), Request{Mode: "read", Source: "A", Doc: "private/secret.md", StartLine: 1, EndLine: 3})
	if err == nil && secret.OK && strings.Contains(secret.Text, "must never appear") {
		t.Fatalf("read bypassed source exclude: %q", secret.Text)
	}
}

func TestRejectTraversalAbsoluteAndSymlinkEscape(t *testing.T) {
	root, outside := fixtureRoot(t), fixtureRoot(t)
	putDoc(t, outside, "outside.md", "private outside marker")
	putDoc(t, root, "inside.md", "safe marker")
	if err := os.Symlink(filepath.Join(outside, "outside.md"), filepath.Join(root, "link.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	lib := newFixtureLibrary(t, []Source{{ID: "safe", Root: root, Extensions: []string{".md"}}})
	for _, doc := range []string{"../outside/outside.md", filepath.Join(outside, "outside.md"), "link.md"} {
		got, err := lib.Execute(context.Background(), Request{Mode: "read", Source: "safe", Doc: doc})
		if err == nil && (got.OK || strings.Contains(got.Text, "private outside marker")) {
			t.Fatalf("unsafe document path accepted (%q): %#v", doc, got)
		}
	}
}

func TestSearchDefaultAnyExplicitAllAnyAndPhraseWithMixedChinese(t *testing.T) {
	root := fixtureRoot(t)
	putDoc(t, root, "derivative.md", "导数与连续性基础 derivative calculus。\n")
	putDoc(t, root, "substitution.md", "积分换元法 substitution。\n")
	putDoc(t, root, "combined.md", "导数与换元联用 derivative substitution。\n")
	putDoc(t, root, "phrase.md", "导数 换元\n")
	lib := newFixtureLibrary(t, []Source{{ID: "notes", Root: root, Extensions: []string{".md"}}})
	for _, tc := range []struct{ name, match, query, want string }{
		{"default-any-mixed", "", "导数 substitution", "积分换元法"},
		{"any-mixed", "any", "导数 substitution", "导数与连续性基础"},
		{"phrase-mixed", "phrase", "积分换元法", "积分换元法"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mustExecute(t, lib, Request{Mode: "search", Query: tc.query, Match: tc.match, Limit: 20})
			resultContaining(t, got, tc.want)
		})
	}
	allMixed := mustExecute(t, lib, Request{Mode: "search", Query: "导数 substitution", Match: "all", Limit: 20})
	if len(allMixed.Results) != 1 || !strings.Contains(allMixed.Results[0].Text, "联用") {
		t.Fatalf("mixed-language all should match only the combined document: %#v", allMixed.Results)
	}

	defaultChinese := mustExecute(t, lib, Request{Mode: "search", Query: "导数 换元", Limit: 20})
	resultContaining(t, defaultChinese, "导数与连续性基础")
	resultContaining(t, defaultChinese, "积分换元法")
	anyChinese := mustExecute(t, lib, Request{Mode: "search", Query: "导数 换元", Match: "any", Limit: 20})
	resultContaining(t, anyChinese, "导数与连续性基础")
	resultContaining(t, anyChinese, "积分换元法")
	allChinese := mustExecute(t, lib, Request{Mode: "search", Query: "导数 换元", Match: "all", Limit: 20})
	if len(allChinese.Results) != 2 {
		t.Fatalf("Chinese all should match the two documents containing both terms: %#v", allChinese.Results)
	}
	for _, result := range allChinese.Results {
		if strings.Contains(result.Doc, "derivative.md") || strings.Contains(result.Doc, "substitution.md") {
			t.Fatalf("Chinese all returned a single-term document: %#v", result)
		}
	}
	phraseChinese := mustExecute(t, lib, Request{Mode: "search", Query: "导数 换元", Match: "phrase", Limit: 20})
	foundPhraseDoc := false
	for _, result := range phraseChinese.Results {
		if result.Doc == "phrase.md" {
			foundPhraseDoc = true
		}
	}
	if !foundPhraseDoc {
		t.Fatalf("Chinese phrase query did not locate phrase.md: %#v", phraseChinese.Results)
	}
}

func TestReadLineRangeAndConfiguredMaximum(t *testing.T) {
	root := fixtureRoot(t)
	putDoc(t, root, "lines.tex", "line-one\nline-two\nline-three\nline-four\nline-five\n")
	lib := newFixtureLibrary(t, []Source{{ID: "tex", Root: root, Extensions: []string{".tex"}}})
	got := mustExecute(t, lib, Request{Mode: "read", Source: "tex", Doc: "lines.tex", StartLine: 2, EndLine: 5})
	if !strings.Contains(got.Text, "line-two") || !strings.Contains(got.Text, "line-three") || !strings.Contains(got.Text, "line-four") || !strings.Contains(got.Text, "line-five") || strings.Contains(got.Text, "line-one") {
		t.Fatalf("line range mismatch: %q", got.Text)
	}
	limited, err := New(Config{Sources: []Source{{ID: "limited", Root: root, Extensions: []string{".tex"}}}, CacheDir: filepath.Join(t.TempDir(), "cache"), Limits: Limits{ReadLines: 2}})
	if err != nil {
		t.Fatal(err)
	}
	capped := mustExecute(t, limited, Request{Mode: "read", Source: "limited", Doc: "lines.tex", StartLine: 1, EndLine: 5})
	if strings.Contains(capped.Text, "line-three") || !strings.Contains(capped.Text, "line-one") || !strings.Contains(capped.Text, "line-two") {
		t.Fatalf("configured read-line maximum not enforced: %q", capped.Text)
	}
}

func TestResultBudgetCountsUnicodeAndReportsTruncation(t *testing.T) {
	root := fixtureRoot(t)
	putDoc(t, root, "long.md", "高等数学关键段落：连续性、导数、积分。\n")
	lib := newFixtureLibrary(t, []Source{{ID: "zh", Root: root, Extensions: []string{".md"}}})
	got := mustExecute(t, lib, Request{Mode: "search", Query: "高等数学", Limit: 10, SnippetChars: 100, MaxChars: 8})
	bodyRunes := 0
	truncationShown := false
	for _, result := range got.Results {
		bodyRunes += len([]rune(result.Text))
		if strings.Contains(result.Text, "…") || strings.Contains(strings.ToLower(result.Text), "trunc") || strings.Contains(result.Text, "截断") {
			truncationShown = true
		}
	}
	for _, warning := range got.Warnings {
		if strings.Contains(warning, "…") || strings.Contains(strings.ToLower(warning), "trunc") || strings.Contains(warning, "截断") {
			truncationShown = true
		}
	}
	if bodyRunes > 8 {
		t.Fatalf("result body exceeds Unicode rune budget: runes=%d results=%#v", bodyRunes, got.Results)
	}
	if len(got.Results) == 0 || !truncationShown {
		t.Fatalf("truncation should be explicit in result text or warnings: %#v", got)
	}
}

func TestCacheReuseContentSHADeletionAndNamespace(t *testing.T) {
	root := fixtureRoot(t)
	path := putDoc(t, root, "cache.md", "xxxxxxxxxxxx\n")
	cache := filepath.Join(t.TempDir(), "cache")
	cfg := Config{Sources: []Source{{ID: "s", Root: root, Extensions: []string{".md"}}}, CacheDir: cache}
	lib, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	first := mustExecute(t, lib, Request{Mode: "search", Query: "xxxxxxxxxxxx", Match: "phrase", Limit: 10})
	second := mustExecute(t, lib, Request{Mode: "search", Query: "xxxxxxxxxxxx", Match: "phrase", Limit: 10})
	if first.Cache.Misses == 0 || second.Cache.Hits == 0 {
		t.Fatalf("expected cold miss then cache hit: first=%#v second=%#v", first.Cache, second.Cache)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("yyyyyyyyyyyy\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	updated := mustExecute(t, lib, Request{Mode: "search", Query: "yyyyyyyyyyyy", Match: "phrase", Limit: 10})
	resultContaining(t, updated, "yyyyyyyyyyyy")
	stale := mustExecute(t, lib, Request{Mode: "search", Query: "xxxxxxxxxxxx", Match: "phrase", Limit: 10})
	if len(stale.Results) != 0 {
		t.Fatalf("same-size/same-mtime replacement retained stale text: %#v", stale.Results)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	deleted := mustExecute(t, lib, Request{Mode: "search", Query: "yyyyyyyyyyyy", Match: "phrase", Limit: 10})
	if len(deleted.Results) != 0 {
		t.Fatalf("deleted file returned stale result: %#v", deleted.Results)
	}

	other, err := New(Config{Sources: []Source{{ID: "s", Root: root, Include: []string{"other/**"}, Extensions: []string{".md"}}}, CacheDir: cache})
	if err != nil {
		t.Fatal(err)
	}
	namespace := mustExecute(t, other, Request{Mode: "list", Recursive: true, Limit: 10})
	if first.Cache.Namespace == "" || namespace.Cache.Namespace == first.Cache.Namespace {
		t.Fatalf("configuration change did not change cache namespace: first=%q other=%q", first.Cache.Namespace, namespace.Cache.Namespace)
	}
}

func TestFrequentPassiveDoesNotIncrementHits(t *testing.T) {
	root := fixtureRoot(t)
	putDoc(t, root, "freq.md", "高频片段用于文档检索缓存。\n")
	lib := newFixtureLibrary(t, []Source{{ID: "freq", Root: root, Extensions: []string{".md"}}})
	seed := mustExecute(t, lib, Request{Mode: "search", Query: "高频片段", Limit: 10})
	entry := resultContaining(t, seed, "高频片段")
	if entry.EntryID == "" {
		t.Fatalf("search result has no stable frequency entry id: %#v", entry)
	}
	passive := mustExecute(t, lib, Request{Mode: "frequent", Passive: true, Limit: 10})
	before := findEntry(t, passive, entry.EntryID).Hits
	passiveAgain := mustExecute(t, lib, Request{Mode: "frequent", Passive: true, Limit: 10})
	unchanged := findEntry(t, passiveAgain, entry.EntryID).Hits
	active := mustExecute(t, lib, Request{Mode: "frequent", Limit: 10})
	activeSnapshot := findEntry(t, active, entry.EntryID).Hits
	after := findEntry(t, mustExecute(t, lib, Request{Mode: "frequent", Passive: true, Limit: 10}), entry.EntryID).Hits
	if before < 1 || unchanged != before || activeSnapshot != before || after != before+1 {
		t.Fatalf("passive/active frequency accounting mismatch: before=%d passiveAgain=%d activeSnapshot=%d after=%d", before, unchanged, activeSnapshot, after)
	}
}

func findEntry(t *testing.T, got Response, id string) Result {
	t.Helper()
	for _, r := range got.Results {
		if r.EntryID == id {
			return r
		}
	}
	t.Fatalf("frequency entry %q not returned: %#v", id, got.Results)
	return Result{}
}

func writeTwoColumnPDF(t *testing.T, path string) {
	t.Helper()
	// Small ASCII-only PDF with two columns and two paragraphs at matching baselines.
	stream := "BT /F1 12 Tf 50 740 Td (LEFT_ALPHA paragraph one.) Tj 0 -24 Td (LEFT_BETA paragraph two.) Tj ET\n" +
		"BT /F1 12 Tf 320 740 Td (RIGHT_GAMMA paragraph one.) Tj 0 -24 Td (RIGHT_DELTA paragraph two.) Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
	}
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, off := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
}

func pdfLibrary(t *testing.T) *Library {
	t.Helper()
	tool, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skipf("PDF text integration requires Poppler pdftotext: %v", err)
	}
	root := fixtureRoot(t)
	writeTwoColumnPDF(t, putEmptyPDFPath(t, root))
	lib := newFixtureLibrary(t, []Source{{ID: "pdf", Root: root, Extensions: []string{".pdf"}}})
	_ = tool
	return lib
}

func putEmptyPDFPath(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "two-column.pdf")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPDFSearchKeepsColumnsSeparate(t *testing.T) {
	lib := pdfLibrary(t)
	first := mustExecute(t, lib, Request{Mode: "search", Query: "LEFT_ALPHA", Limit: 20})
	resultContaining(t, first, "LEFT_ALPHA")
	second := mustExecute(t, lib, Request{Mode: "search", Query: "RIGHT_DELTA", Limit: 20})
	resultContaining(t, second, "RIGHT_DELTA")
	if first.Cache.Misses == 0 || second.Cache.Hits == 0 {
		t.Fatalf("unchanged PDF should reuse extracted/indexed content: first=%#v second=%#v", first.Cache, second.Cache)
	}
	got := mustExecute(t, lib, Request{Mode: "search", Query: "LEFT_BETA RIGHT_GAMMA", Match: "phrase", Limit: 20})
	for _, r := range got.Results {
		if strings.Contains(r.Text, "LEFT_BETA") && strings.Contains(r.Text, "RIGHT_GAMMA") {
			t.Fatalf("cross-column text was joined into a false phrase: %#v", r)
		}
	}
	left := mustExecute(t, lib, Request{Mode: "search", Query: "LEFT_BETA", Limit: 20})
	resultContaining(t, left, "LEFT_BETA")
}

func TestPDFViewReturnsBoundedCrop(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skipf("PDF view integration requires Poppler pdftoppm: %v", err)
	}
	lib := pdfLibrary(t)
	out := filepath.Join(t.TempDir(), "view")
	got := mustExecute(t, lib, Request{Mode: "view", Source: "pdf", Doc: "two-column.pdf", Page: 1, Region: &Region{Left: 5, Top: 5, Right: 48, Bottom: 45}, OutDir: out, DPI: 110})
	if len(got.Images) == 0 {
		t.Fatalf("view returned no image: %#v", got)
	}
	img := got.Images[0]
	if img.Width <= 0 || img.Height <= 0 || img.MIME != "image/png" {
		t.Fatalf("invalid crop metadata: %#v", img)
	}
	if filepath.Base(img.Path) == "two-column.pdf" || !strings.HasPrefix(filepath.Clean(img.Path), filepath.Clean(out)+string(os.PathSeparator)) {
		t.Fatalf("view output escaped its requested directory or replaced source: %#v", img)
	}
}
