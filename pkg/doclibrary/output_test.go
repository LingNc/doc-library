package doclibrary

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func boolValue(v bool) *bool { return &v }

func TestOutputPrecedenceAndValidation(t *testing.T) {
	for _, cv := range []int{0, 1, 2} {
		for _, cb := range []*bool{nil, boolValue(true), boolValue(false)} {
			lib, err := New(Config{CacheDir: t.TempDir(), Output: OutputSettings{Version: cv, IncludeBBox: cb}})
			if err != nil {
				t.Fatal(err)
			}
			for _, rv := range []int{0, 1, 2} {
				for _, rb := range []*bool{nil, boolValue(true), boolValue(false)} {
					r, err := lib.normalizeRequest(Request{Mode: "schema", OutputVersion: rv, IncludeBBox: rb})
					if err != nil {
						t.Fatal(err)
					}
					wantVersion := rv
					if wantVersion == 0 {
						wantVersion = cv
					}
					if wantVersion == 0 {
						wantVersion = 1
					}
					wantBBox := wantVersion == 1
					if cb != nil {
						wantBBox = *cb
					}
					if rb != nil {
						wantBBox = *rb
					}
					if r.OutputVersion != wantVersion || r.IncludeBBox == nil || *r.IncludeBBox != wantBBox {
						t.Fatalf("config=%d/%v request=%d/%v: got=%d/%v want=%d/%v", cv, cb, rv, rb, r.OutputVersion, r.IncludeBBox, wantVersion, wantBBox)
					}
					resp := coreExecute(t, lib, Request{Mode: "schema", OutputVersion: rv, IncludeBBox: rb})
					wantProtocol := Version
					if wantVersion == 2 {
						wantProtocol = CompactVersion
					}
					if resp.Version != wantProtocol {
						t.Fatalf("effective response version=%s want %s", resp.Version, wantProtocol)
					}
				}
			}
		}
	}
	for _, invalid := range []int{-1, 3, 99} {
		if _, err := New(Config{Output: OutputSettings{Version: invalid}}); err == nil {
			t.Fatalf("accepted config version %d", invalid)
		}
		lib, err := New(Config{CacheDir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := lib.Execute(context.Background(), Request{Mode: "schema", OutputVersion: invalid}); err == nil {
			t.Fatalf("accepted request version %d", invalid)
		}
	}
}

func TestLegacyMarshalExactWire(t *testing.T) {
	bb, pct := [4]float64{1, 2, 3, 4}, [4]float64{10, 20, 30, 40}
	next := 2
	r := Response{Version: Version, OK: true, Mode: "search", Text: "body mirror", Results: []Result{{Source: "s", Doc: "a.pdf", Kind: "pdf", Page: 1, BBox: &bb, BBoxPct: &pct, PageWidth: 10, PageHeight: 20, StartLine: 1, EndLine: 2, Text: "body", Score: 1, EntryID: "id", Hits: 2, Truncated: true}}, Images: []Image{}, Cache: CacheInfo{Namespace: "ns", Hits: 1, Misses: 2, Files: 3, Fingerprint: "fp"}, Warnings: []string{"warning"}, Truncated: true, NextOffset: &next, Schema: map[string]any{"a": true}}
	got, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":"doc-library/1","ok":true,"mode":"search","text":"body mirror","results":[{"source":"s","doc":"a.pdf","kind":"pdf","page":1,"bbox":[1,2,3,4],"bbox_pct":[10,20,30,40],"page_width":10,"page_height":20,"start_line":1,"end_line":2,"text":"body","score":1,"entry_id":"id","hits":2,"truncated":true}],"images":[],"cache":{"namespace":"ns","hits":1,"misses":2,"files":3,"fingerprint":"fp"},"warnings":["warning"],"truncated":true,"next_offset":2,"schema":{"a":true}}`
	if string(got) != want {
		t.Fatalf("legacy wire changed:\n%s\nwant:\n%s", got, want)
	}
}

func TestCompactMarshalSingleBodyAndDiagnosticWarnings(t *testing.T) {
	bb := [4]float64{1, 2, 3, 4}
	r := Response{Version: CompactVersion, OK: true, Mode: "search", Text: "unique-body", Results: []Result{{Source: "s", Doc: "a.pdf", Kind: "pdf", Page: 3, StartLine: 5, EndLine: 6, Text: "unique-body", BBox: &bb, BBoxPct: &bb, PageWidth: 500, PageHeight: 700}, {Source: "s2", Doc: "a.tex", Kind: "source", StartLine: 7, EndLine: 9}}, Images: []Image{}, Cache: CacheInfo{Namespace: "private-cache"}, Warnings: []string{"s:unknown: binary or invalid UTF-8 skipped", "s:other: skipped binary file", "PDF page has no extractable text; no OCR is performed", "frequency cache was invalid; starting with an empty history", "result snippet truncated"}}
	got, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(got), "unique-body") != 1 {
		t.Fatalf("body mirrored: %s", got)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(got, &wire); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"text", "cache"} {
		if _, ok := wire[key]; ok {
			t.Fatalf("unexpected %s: %s", key, got)
		}
	}
	var results []map[string]json.RawMessage
	if err := json.Unmarshal(wire["results"], &results); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"bbox", "page_width", "page_height", "start_line", "end_line"} {
		if _, ok := results[0][key]; ok {
			t.Fatalf("PDF %s leaked into v2: %s", key, got)
		}
	}
	if results[0]["bbox_pct"] == nil || string(results[1]["start_line"]) != "7" || string(results[1]["end_line"]) != "9" {
		t.Fatalf("optional region or source lines lost: %s", got)
	}
	var warnings []string
	if err := json.Unmarshal(wire["warnings"], &warnings); err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 3 {
		t.Fatalf("important warnings lost or binary skips repeated: %v", warnings)
	}
	if r.Text != "unique-body" || r.Results[0].StartLine != 5 || r.Cache.Namespace == "" {
		t.Fatal("marshal mutated Go response")
	}
	for _, mode := range []string{"status", "index"} {
		r.Mode = mode
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"cache":`) {
			t.Fatalf("%s lost diagnostic cache", mode)
		}
	}
}

func TestOutputCacheIdentityMatchesLegacyAndSharesHistory(t *testing.T) {
	base, root, cache := coreFixture(t, map[string]string{"a.txt": "needle shared content\n"})
	quotedRoot, _ := json.Marshal(root)
	quotedCache, _ := json.Marshal(cache)
	// Frozen pre-output Config bytes, including its historical field order and
	// normalized defaults. Output must not even appear as an empty object.
	legacy := fmt.Sprintf(`{"Schema":%q,"Config":{"sources":[{"id":"a","root":%s,"weight":1}],"cache_dir":%s,"cache_mode":"readwrite","search":{"match":"any","limit":5,"snippet_chars":1200,"max_chars":6000},"limits":{"max_file_bytes":268435456,"max_results":1000,"read_lines":400},"frequency":{"max_entries":256,"text_chars":2048},"pdf":{"pdftotext":"pdftotext","pdftoppm":"pdftoppm","timeout_seconds":120}}}`, extractionSchema, quotedRoot, quotedCache)
	if base.namespace != hashBytes([]byte(legacy)) {
		t.Fatalf("pre-output namespace changed: got=%s legacy=%s bytes=%s", base.namespace, hashBytes([]byte(legacy)), legacy)
	}
	cold := coreExecute(t, base, Request{Mode: "index"})
	if cold.Cache.Misses != 1 {
		t.Fatal(cold.Cache)
	}
	calls := 0
	for _, version := range []int{0, 1, 2} {
		for _, bbox := range []*bool{nil, boolValue(true), boolValue(false)} {
			cfg := Config{Sources: []Source{{ID: "a", Root: root}}, CacheDir: cache, Output: OutputSettings{Version: version, IncludeBBox: bbox}}
			lib, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if lib.namespace != base.namespace {
				t.Fatalf("output split namespace: version=%d bbox=%v", version, bbox)
			}
			for _, requestBBox := range []*bool{nil, boolValue(true), boolValue(false)} {
				got := coreExecute(t, lib, Request{Query: "needle", OutputVersion: version, IncludeBBox: requestBBox})
				calls++
				if got.Cache.Hits != 1 || got.Cache.Misses != 0 || got.Cache.Fingerprint != cold.Cache.Fingerprint {
					t.Fatalf("output caused extraction or changed key: %+v", got.Cache)
				}
			}
		}
	}
	frequent := coreExecute(t, base, Request{Mode: "frequent", Passive: true})
	if len(frequent.Results) != 1 || frequent.Results[0].Hits != calls {
		t.Fatalf("history split by output settings: %#v want %d hits", frequent.Results, calls)
	}
}

func TestPDFOutputProjectionDoesNotChangeCacheOrAutomaticCrop(t *testing.T) {
	lib := pdfLibrary(t)
	legacy := coreExecute(t, lib, Request{Query: "LEFT_ALPHA", Limit: 1})
	first := legacy.Results[0]
	if first.BBox == nil || first.BBoxPct == nil || first.PageWidth == 0 {
		t.Fatal("legacy default lost geometry")
	}
	for _, tc := range []struct {
		version    int
		bbox       *bool
		point, pct bool
	}{
		{1, nil, true, true}, {1, boolValue(false), false, false}, {1, boolValue(true), true, true},
		{2, nil, false, false}, {2, boolValue(false), false, false}, {2, boolValue(true), false, true},
	} {
		got := coreExecute(t, lib, Request{Query: "LEFT_ALPHA", Limit: 1, OutputVersion: tc.version, IncludeBBox: tc.bbox})
		r := got.Results[0]
		if (r.BBox != nil) != tc.point || (r.BBoxPct != nil) != tc.pct || (r.PageWidth > 0) != tc.point || (r.PageHeight > 0) != tc.point {
			t.Fatalf("projection=%d/%v got=%+v", tc.version, tc.bbox, r)
		}
		if r.Source != first.Source || r.Doc != first.Doc || r.Page != first.Page || r.Text != first.Text || r.EntryID != first.EntryID || r.StartLine != first.StartLine || r.EndLine != first.EndLine {
			t.Fatalf("output changed canonical hit: %+v vs %+v", r, first)
		}
		if got.Cache.Hits != 1 || got.Cache.Misses != 0 || got.Cache.Fingerprint != legacy.Cache.Fingerprint {
			t.Fatal("output invalidated PDF cache")
		}
		if tc.version == 2 && (strings.Contains(got.Text, "blocks") || strings.Contains(got.Text, "page_width") || strings.Contains(got.Text, "bbox_pct") != tc.pct) {
			t.Fatalf("compact host text: %s", got.Text)
		}
	}
	// Search history historically stores PDF block identity without geometry.
	// Asking for bbox must return the available entry without re-extracting.
	cfg := lib.config
	cfg.PDF.PDFToText = "/not-a-working-pdftotext"
	lib.config.PDF.PDFToText = cfg.PDF.PDFToText
	frequent := coreExecute(t, lib, Request{Mode: "frequent", OutputVersion: 2, IncludeBBox: boolValue(true), Passive: true})
	if len(frequent.Results) == 0 || frequent.Results[0].BBoxPct != nil {
		t.Fatalf("missing frequent bbox should stay absent: %+v", frequent.Results)
	}
	lib.config.PDF.PDFToText = "pdftotext"
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skipf("crop integration requires Poppler pdftoppm: %v", err)
	}
	cropped := coreExecute(t, lib, Request{Query: "LEFT_ALPHA", Limit: 1, OutputVersion: 2, IncludeBBox: boolValue(false), IncludeImage: true, DPI: 72, OutDir: filepath.Join(t.TempDir(), "crop")})
	if len(cropped.Images) != 1 || cropped.Results[0].BBox != nil || cropped.Results[0].BBoxPct != nil {
		t.Fatal("hidden geometry prevented rendering or leaked into output")
	}
	b := *first.BBoxPct
	explicit := coreExecute(t, lib, Request{Mode: "view", Doc: first.Doc, Page: first.Page, OutputVersion: 2, IncludeBBox: boolValue(false), Region: &Region{Left: maxF(0, b[0]-4), Top: maxF(0, b[1]-2), Right: minF(100, b[2]+4), Bottom: minF(100, b[3]+2)}, DPI: 72, OutDir: filepath.Join(t.TempDir(), "view")})
	a, err := os.ReadFile(cropped.Images[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	bimage, err := os.ReadFile(explicit.Images[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, bimage) || cropped.Images[0].Width >= int(first.PageWidth) {
		t.Fatal("automatic crop no longer matches explicit internal PDF region")
	}
}

func TestOutputSchemaDescribesBothWireShapes(t *testing.T) {
	s := Schema()
	rp := s["request"].(map[string]any)["properties"].(map[string]any)
	if rp["include_bbox"] == nil || !reflect.DeepEqual(rp["output_version"].(map[string]any)["enum"], []int{0, 1, 2}) {
		t.Fatal("request schema missing output fields or versions")
	}
	branches := s["response"].(map[string]any)["oneOf"].([]any)
	legacy := branches[0].(map[string]any)["properties"].(map[string]any)
	compact := branches[1].(map[string]any)["properties"].(map[string]any)
	if legacy["text"] == nil || legacy["cache"] == nil || compact["text"] != nil {
		t.Fatal("wire schemas do not reflect actual MarshalJSON shapes")
	}
	results := compact["results"].(map[string]any)["items"].(map[string]any)["oneOf"].([]any)
	pdf := results[0].(map[string]any)["properties"].(map[string]any)
	if pdf["bbox"] != nil || pdf["page_width"] != nil || pdf["start_line"] != nil || pdf["bbox_pct"] == nil || pdf["page"] == nil {
		t.Fatal("compact PDF wire schema inaccurate")
	}
}
