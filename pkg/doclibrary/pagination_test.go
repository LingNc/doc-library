package doclibrary

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func resultKeys(results []Result) []string {
	keys := make([]string, 0, len(results))
	for _, r := range results {
		keys = append(keys, r.Source+":"+r.Doc+":"+r.EntryID)
	}
	return keys
}

func assertContinuationWalk(t *testing.T, lib *Library, request Request, want []string, budgetStops bool) {
	t.Helper()
	request.Offset = 0
	request.MaxChars = 4
	request.Limit = 3
	var seen []string
	for page := 0; page < 5; page++ {
		got := coreExecute(t, lib, request)
		seen = append(seen, resultKeys(got.Results)...)
		if budgetStops && len(got.Results) != 1 {
			t.Fatalf("budget early page returned %d results: %+v", len(got.Results), got)
		}
		if got.NextOffset == nil {
			if !reflect.DeepEqual(seen, want) {
				t.Fatalf("walk reordered, duplicated, or dropped candidates: got=%v want=%v", seen, want)
			}
			return
		}
		if *got.NextOffset <= request.Offset || *got.NextOffset != len(seen) {
			t.Fatalf("continuation did not consume returned candidates: offset=%d next=%d seen=%v", request.Offset, *got.NextOffset, seen)
		}
		if !got.Truncated && budgetStops {
			t.Fatal("budget early stop did not report truncation")
		}
		request.Offset = *got.NextOffset
	}
	t.Fatal("continuation loop")
}

func TestPaginationAcrossModesBudgetBoundaryLastAndEmpty(t *testing.T) {
	lib, _, _ := coreFixture(t, map[string]string{
		"a.txt": "needle first candidate paragraph\n", "b.txt": "needle second candidate paragraph\n", "c.txt": "needle third candidate paragraph\n",
	})
	coreExecute(t, lib, Request{Query: "needle", Limit: 3})
	for _, mode := range []string{"search", "grep", "list", "find", "status", "index", "frequent"} {
		t.Run(mode, func(t *testing.T) {
			r := Request{Mode: mode, Query: "needle", Limit: 3, MaxChars: 1000, Recursive: true, Passive: mode == "frequent", OutputVersion: 2}
			if mode == "find" {
				r.Query = "*.txt"
			}
			baseline := coreExecute(t, lib, r)
			if len(baseline.Results) != 3 || baseline.NextOffset != nil || baseline.Truncated {
				t.Fatalf("exact-limit terminal page invented continuation/truncation: %+v", baseline)
			}
			want := resultKeys(baseline.Results)
			assertContinuationWalk(t, lib, r, want, mode != "list" && mode != "find")
			for _, offset := range []int{2, 3, 10} {
				r.Offset, r.Limit = offset, 1
				got := coreExecute(t, lib, r)
				if got.NextOffset != nil {
					t.Fatalf("last/empty page invented continuation: %+v", got)
				}
				wantCount := 0
				if offset == 2 {
					wantCount = 1
				}
				if len(got.Results) != wantCount {
					t.Fatalf("offset=%d got %d results", offset, len(got.Results))
				}
			}
			r.Offset, r.Limit = 0, 1
			first := coreExecute(t, lib, r)
			if first.NextOffset == nil || *first.NextOffset != 1 {
				t.Fatalf("actual next candidate missing: %+v", first)
			}
		})
	}
}

func TestSnippetTruncationIsNotPaginationAndExactTextBudgetIsComplete(t *testing.T) {
	lib, _, _ := coreFixture(t, map[string]string{"a.txt": "needle one\n"})
	for _, mode := range []string{"search", "grep", "frequent"} {
		if mode == "frequent" {
			coreExecute(t, lib, Request{Query: "needle"})
		}
		r := Request{Mode: mode, Query: "needle", Limit: 1, SnippetChars: 2, LineChars: 2, MaxChars: 100, Passive: true}
		got := coreExecute(t, lib, r)
		if !got.Truncated || len(got.Results) != 1 || !got.Results[0].Truncated || got.NextOffset != nil {
			t.Fatalf("%s snippet truncation confused with more items: %+v", mode, got)
		}
		r.SnippetChars, r.LineChars, r.MaxChars = 100, 100, len([]rune("needle one"))
		got = coreExecute(t, lib, r)
		if got.Truncated || got.NextOffset != nil {
			t.Fatalf("%s exact text budget incorrectly incomplete: %+v", mode, got)
		}
	}
}

func TestPaginationRecordsOnlyReturnedCandidates(t *testing.T) {
	for _, mode := range []string{"search", "grep"} {
		t.Run(mode, func(t *testing.T) {
			lib, _, _ := coreFixture(t, map[string]string{"a.txt": "needle alpha\n", "b.txt": "needle beta\n", "c.txt": "needle gamma\n"})
			first := coreExecute(t, lib, Request{Mode: mode, Query: "needle", Limit: 3, MaxChars: 3})
			if len(first.Results) != 1 || first.NextOffset == nil || *first.NextOffset != 1 {
				t.Fatal(first)
			}
			st, _, err := lib.readFrequency()
			if err != nil {
				t.Fatal(err)
			}
			if len(st.Entries) != 1 || st.Entries[0].Result.Doc != first.Results[0].Doc || st.Entries[0].Result.Hits != 1 {
				t.Fatalf("lookahead or budget-excluded candidates counted: %+v", st)
			}
			second := coreExecute(t, lib, Request{Mode: mode, Query: "needle", Limit: 3, Offset: *first.NextOffset, MaxChars: 3})
			if len(second.Results) != 1 || second.Results[0].Doc == first.Results[0].Doc {
				t.Fatal(second)
			}
			st, _, err = lib.readFrequency()
			if err != nil {
				t.Fatal(err)
			}
			if len(st.Entries) != 2 {
				t.Fatalf("continuation pre-counted later candidates: %+v", st)
			}
		})
	}
	lib, _, _ := coreFixture(t, map[string]string{"a.txt": "needle alpha\n", "b.txt": "needle beta\n", "c.txt": "needle gamma\n"})
	coreExecute(t, lib, Request{Query: "needle", Limit: 3})
	first := coreExecute(t, lib, Request{Mode: "frequent", Limit: 3, MaxChars: 3})
	if len(first.Results) != 1 || first.NextOffset == nil {
		t.Fatal(first)
	}
	st, _, err := lib.readFrequency()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range st.Entries {
		want := 1
		if entry.Result.EntryID == first.Results[0].EntryID {
			want = 2
		}
		if entry.Result.Hits != want {
			t.Fatalf("frequent counted unreturned entry: %+v", entry)
		}
	}
}

func TestPDFPaginationBudgetAndSnippetPreserveCanonicalBlocks(t *testing.T) {
	lib := pdfLibrary(t)
	root := lib.config.Sources[0].Root
	original, err := os.ReadFile(filepath.Join(root, "two-column.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	putDoc(t, root, "b.pdf", string(original))
	putDoc(t, root, "c.pdf", string(original))
	r := Request{Query: "LEFT_ALPHA", Limit: 3, MaxChars: 1000, OutputVersion: 2}
	baseline := coreExecute(t, lib, r)
	if len(baseline.Results) != 3 || baseline.NextOffset != nil {
		t.Fatal(baseline)
	}
	for _, result := range baseline.Results {
		if result.Page != 1 || result.StartLine == 0 || result.EntryID == "" {
			t.Fatalf("internal PDF identity lost: %+v", result)
		}
	}
	assertContinuationWalk(t, lib, r, resultKeys(baseline.Results), true)
	r.SnippetChars = 3
	snippets := coreExecute(t, lib, r)
	if !snippets.Truncated || snippets.NextOffset != nil || len(snippets.Results) != 3 {
		t.Fatalf("PDF snippet truncation invented continuation: %+v", snippets)
	}
	for _, result := range snippets.Results {
		if !result.Truncated || result.Page == 0 || result.StartLine == 0 {
			t.Fatalf("PDF snippet lost location or truncation: %+v", result)
		}
	}
	frequent := coreExecute(t, lib, Request{Mode: "frequent", Limit: 10, Passive: true, OutputVersion: 2})
	if len(frequent.Results) != 3 {
		t.Fatalf("PDF lookahead recorded spurious blocks: %+v", frequent.Results)
	}
	for _, result := range frequent.Results {
		if result.Hits != 3 {
			t.Fatalf("PDF returned blocks should be counted once per actual response: %+v", result)
		}
	}
}

func TestReadBudgetReportsActualStopAndKeepsConfiguredLineCap(t *testing.T) {
	lib := pdfLibrary(t)
	all := coreExecute(t, lib, Request{Mode: "read", Doc: "two-column.pdf", Page: 1})
	if len(all.Results) < 2 {
		t.Fatal("PDF fixture lacks multiple blocks")
	}
	firstChars := len([]rune(all.Results[0].Text))
	got := coreExecute(t, lib, Request{Mode: "read", Doc: "two-column.pdf", Page: 1, MaxChars: firstChars})
	if len(got.Results) != 1 || !got.Truncated || got.NextOffset != nil {
		t.Fatalf("PDF read budget did not identify an actual stop: %+v", got)
	}
	last := all.Results[len(all.Results)-1]
	got = coreExecute(t, lib, Request{Mode: "read", Doc: "two-column.pdf", Page: 1, StartLine: last.StartLine, EndLine: last.EndLine, MaxChars: len([]rune(last.Text))})
	if got.Truncated || got.NextOffset != nil {
		t.Fatalf("exact PDF block budget falsely truncated: %+v", got)
	}
	files := map[string]string{"a.txt": ""}
	for i := 0; i < 450; i++ {
		files["a.txt"] += "line\n"
	}
	textlib, _, _ := coreFixture(t, files)
	text := coreExecute(t, textlib, Request{Mode: "read", Doc: "a.txt"})
	if len(text.Results) != 1 || text.Results[0].StartLine != 1 || text.Results[0].EndLine != 400 {
		t.Fatalf("default 400-line source read changed: %+v", text.Results)
	}
}
