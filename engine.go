// Search and navigation adapt the paragraph search/reading/merge algorithms of
// compare-figure go/luadraw/docs.go and doc_files.go, MIT (c) 2026 LingNc.
package doclibrary

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type textMatcher struct {
	query     string
	tokens    []string
	mode      string
	sensitive bool
	re        *regexp.Regexp
}

func normalized(s string, sensitive bool) string {
	rs := []rune(strings.Join(strings.Fields(s), " "))
	out := make([]rune, 0, len(rs))
	for i, r := range rs {
		if r == ' ' && i > 0 && i+1 < len(rs) && isCJK(rs[i-1]) && isCJK(rs[i+1]) {
			continue
		}
		out = append(out, r)
	}
	s = string(out)
	if !sensitive {
		s = strings.ToLower(s)
	}
	return s
}
func terms(s string) []string {
	var tokens []string
	var run []rune
	class := 0
	flush := func() {
		if len(run) > 0 {
			tokens = append(tokens, string(run))
			run = nil
		}
	}
	for _, r := range s {
		next := 0
		if isCJK(r) {
			next = 2
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			next = 1
		}
		if next == 0 {
			flush()
			class = 0
			continue
		}
		if class != next {
			flush()
		}
		class = next
		run = append(run, r)
	}
	flush()
	if len(tokens) == 0 && strings.TrimSpace(s) != "" {
		tokens = []string{s}
	}
	return tokens
}
func newMatcher(r Request) (textMatcher, error) {
	m := textMatcher{query: normalized(r.Query, r.CaseSensitive), mode: r.Match, sensitive: r.CaseSensitive}
	queryTerms := strings.Join(strings.Fields(r.Query), " ")
	if !r.CaseSensitive {
		queryTerms = strings.ToLower(queryTerms)
	}
	m.tokens = terms(queryTerms)
	if r.Regex {
		pattern := r.Query
		if !r.CaseSensitive {
			pattern = "(?i)" + pattern
		}
		re, e := regexp.Compile(pattern)
		if e != nil {
			return m, fmt.Errorf("invalid query regex: %w", e)
		}
		m.re = re
	}
	return m, nil
}
func (m textMatcher) score(text string) int {
	if m.re != nil {
		if m.re.MatchString(text) {
			return 1
		}
		return 0
	}
	text = normalized(text, m.sensitive)
	if m.mode == "phrase" {
		if strings.Contains(text, m.query) {
			return 1000
		}
		return 0
	}
	score := 0
	for _, t := range m.tokens {
		if strings.Contains(text, t) {
			score++
		} else if m.mode == "all" {
			return 0
		}
	}
	if strings.Contains(text, m.query) {
		score += 1000
	}
	return score
}
func (l *Library) normalizeRequest(r Request) (Request, error) {
	if r.Mode == "" {
		r.Mode = "search"
	}
	switch r.Mode {
	case "search", "read", "view", "list", "find", "grep", "frequent", "index", "status", "schema":
	default:
		return r, fmt.Errorf("unknown mode %q", r.Mode)
	}
	if r.Offset < 0 || r.Limit < 0 || r.SnippetChars < 0 || r.MaxChars < 0 || r.LineChars < 0 || r.StartLine < 0 || r.EndLine < 0 || r.Page < 0 || r.DPI < 0 || r.Zoom < 0 {
		return r, fmt.Errorf("request limits and locations cannot be negative")
	}
	if r.EndLine > 0 && r.StartLine > r.EndLine {
		return r, fmt.Errorf("end_line must not precede start_line")
	}
	if r.Limit == 0 {
		switch r.Mode {
		case "list", "find", "index", "status":
			r.Limit = minInt(100, l.config.Limits.MaxResults)
		case "frequent":
			r.Limit = minInt(8, l.config.Limits.MaxResults)
		case "grep":
			r.Limit = minInt(50, l.config.Limits.MaxResults)
		default:
			r.Limit = l.config.Search.Limit
		}
	}
	if r.Limit > l.config.Limits.MaxResults {
		return r, fmt.Errorf("limit exceeds configured max_results (%d)", l.config.Limits.MaxResults)
	}
	if r.SnippetChars == 0 {
		r.SnippetChars = l.config.Search.SnippetChars
	}
	if r.MaxChars == 0 {
		r.MaxChars = l.config.Search.MaxChars
	}
	if r.Match == "" {
		if r.Mode == "grep" {
			r.Match = "phrase"
		} else {
			r.Match = l.config.Search.Match
		}
	}
	if r.Match != "any" && r.Match != "all" && r.Match != "phrase" {
		return r, fmt.Errorf("match must be any, all, or phrase")
	}
	for _, kind := range r.Kinds {
		if kind != "source" && kind != "pdf" {
			return r, fmt.Errorf("kinds accepts source or pdf")
		}
	}
	if r.DPI > 600 || r.Zoom > 5000 {
		return r, fmt.Errorf("DPI must be <=600 and zoom <=5000")
	}
	if r.DPI > 0 && r.Zoom > 0 {
		return r, fmt.Errorf("dpi and zoom are mutually exclusive")
	}
	if r.Region != nil {
		v := r.Region
		for _, x := range []float64{v.Left, v.Top, v.Right, v.Bottom} {
			if math.IsNaN(x) || math.IsInf(x, 0) || x < 0 || x > 100 {
				return r, fmt.Errorf("region values must be finite percentages 0..100")
			}
		}
		if v.Right <= v.Left || v.Bottom <= v.Top {
			return r, fmt.Errorf("region must have positive area")
		}
	}
	if r.Mode == "search" || r.Mode == "grep" || r.Mode == "find" {
		if strings.TrimSpace(r.Query) == "" {
			return r, fmt.Errorf("%s requires query", r.Mode)
		}
	}
	return r, nil
}

func (l *Library) Execute(ctx context.Context, r Request) (Response, error) {
	resp := Response{Version: Version, OK: false, Results: []Result{}, Images: []Image{}, Cache: CacheInfo{Namespace: l.namespace}}
	var e error
	r, e = l.normalizeRequest(r)
	resp.Mode = r.Mode
	if e != nil {
		return resp, e
	}
	if e = ctx.Err(); e != nil {
		return resp, e
	}
	if r.Mode == "schema" {
		resp.OK = true
		resp.Schema = Schema()
		return resp, nil
	}
	sources, e := l.selected(r)
	if e != nil {
		return resp, e
	}
	if len(sources) == 0 {
		return resp, fmt.Errorf("at least one configured source is required")
	}
	var records []frequencyEntry
	switch r.Mode {
	case "frequent":
		resp.Results, records, resp.Warnings, e = l.frequent(ctx, r, sources)
		if r.Passive {
			records = nil
		}
	case "read", "view":
		resp.Results, resp.Images, records, resp.Warnings, e = l.readOrView(ctx, r, sources, &resp.Cache)
	default:
		var files []sourceFile
		files, e = l.scan(ctx, sources, r.Doc)
		if e != nil {
			return resp, e
		}
		filtered := files[:0]
		for _, f := range files {
			if requestAllows(r, f.doc) {
				filtered = append(filtered, f)
			}
		}
		files = filtered
		switch r.Mode {
		case "list", "find", "status":
			resp.Cache.Files = len(files)
			resp.Results, e = l.navigate(r, files)
		case "grep":
			resp.Results, records, resp.Warnings, e = l.grep(ctx, r, files, &resp.Cache)
		case "search", "index":
			var docs []document
			docs, resp.Warnings, e = l.loadAll(ctx, files, &resp.Cache)
			if e != nil {
				return resp, e
			}
			if r.Mode == "search" {
				resp.Results, records, e = l.searchDocuments(r, docs)
			} else {
				for _, d := range docs {
					resp.Results = append(resp.Results, Result{Source: d.file.source.ID, Doc: d.file.doc, Kind: d.entry.Kind, Text: d.digest})
				}
				resp.Results = budgetResults(pageResults(resp.Results, r), r.MaxChars)
			}
			if e == nil && r.Mode == "search" && r.IncludeImage {
				for _, result := range resp.Results {
					if result.Kind != "pdf" {
						continue
					}
					for _, d := range docs {
						if d.file.source.ID == result.Source && d.file.doc == result.Doc {
							imageRequest := r
							if r.Region == nil && result.BBoxPct != nil {
								b := *result.BBoxPct
								imageRequest.Region = &Region{Left: maxF(0, b[0]-4), Top: maxF(0, b[1]-2), Right: minF(100, b[2]+4), Bottom: minF(100, b[3]+2)}
							}
							image, err := l.render(ctx, d, d.entry.Pages[result.Page-1], imageRequest)
							if err != nil {
								return resp, err
							}
							resp.Images = append(resp.Images, image)
							break
						}
					}
					break
				}
			}
		}
	}
	if e != nil {
		return resp, e
	}
	if len(records) > 0 {
		warnings, err := l.recordFrequency(ctx, records)
		if err != nil {
			return resp, fmt.Errorf("frequency cache update failed: %w", err)
		}
		resp.Warnings = append(resp.Warnings, warnings...)
	}
	if resp.Results == nil {
		resp.Results = []Result{}
	}
	if resp.Images == nil {
		resp.Images = []Image{}
	}
	used := 0
	for _, result := range resp.Results {
		used += len([]rune(result.Text))
		if result.Truncated {
			resp.Truncated = true
		}
	}
	if resp.Truncated {
		resp.Warnings = append(resp.Warnings, "result snippet truncated; increase snippet_chars, line_chars, or max_chars to read more")
	}
	if used >= r.MaxChars {
		resp.Truncated = true
		resp.Warnings = append(resp.Warnings, "result text budget reached; increase max_chars or narrow the request")
	}
	if len(resp.Results) >= r.Limit && (r.Mode == "search" || r.Mode == "grep" || r.Mode == "list" || r.Mode == "find" || r.Mode == "index" || r.Mode == "status" || r.Mode == "frequent") {
		next := r.Offset + len(resp.Results)
		resp.NextOffset = &next
		resp.Warnings = append(resp.Warnings, fmt.Sprintf("result limit reached; more results may exist at offset=%d", next))
	}
	resp.Text = formatResults(resp.Results)
	resp.OK = true
	return resp, nil
}
func requestAllows(r Request, doc string) bool {
	kind := "source"
	if strings.EqualFold(filepath.Ext(doc), ".pdf") {
		kind = "pdf"
	}
	if len(r.Kinds) > 0 {
		ok := false
		for _, k := range r.Kinds {
			if k == kind {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	if len(r.Extensions) > 0 {
		ext := strings.ToLower(filepath.Ext(doc))
		ok := false
		for _, x := range r.Extensions {
			if !strings.HasPrefix(x, ".") {
				x = "." + x
			}
			if strings.EqualFold(x, ext) {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	return true
}
func pageResults(results []Result, r Request) []Result {
	if r.Offset >= len(results) {
		return []Result{}
	}
	return results[r.Offset:minInt(len(results), r.Offset+r.Limit)]
}
func formatResults(results []Result) string {
	var rows []string
	for _, r := range results {
		where := r.Source + ":" + r.Doc
		if r.Page > 0 {
			where += fmt.Sprintf("#page=%d blocks %d-%d", r.Page, r.StartLine, r.EndLine)
		} else if r.StartLine > 0 {
			where += fmt.Sprintf(":%d-%d", r.StartLine, r.EndLine)
		}
		if r.Text != "" {
			where += "\n" + r.Text
		}
		rows = append(rows, where)
	}
	return strings.Join(rows, "\n\n")
}
func resultFor(d document, h hit) Result {
	result := Result{Source: d.file.source.ID, Doc: h.doc, Kind: h.kind, Page: h.page, StartLine: h.line, EndLine: h.end, Text: h.text, Score: float64(h.score) * d.file.source.Weight, EntryID: entryID(d.file.source.ID, h.doc, h.page, h.line, h.end)}
	if h.box != nil {
		b := *h.box
		result.BBox = &b
		result.PageWidth = h.info.Width
		result.PageHeight = h.info.Height
		pct := [4]float64{b[0] / h.info.Width * 100, b[1] / h.info.Height * 100, b[2] / h.info.Width * 100, b[3] / h.info.Height * 100}
		result.BBoxPct = &pct
	}
	return result
}

type candidate struct {
	result  Result
	records []frequencyEntry
}

func (l *Library) searchDocuments(r Request, docs []document) ([]Result, []frequencyEntry, error) {
	matcher, e := newMatcher(r)
	if e != nil {
		return nil, nil, e
	}
	var candidates []candidate
	for _, d := range docs {
		var hits []hit
		if d.entry.Kind == "source" {
			for _, p := range sourceParagraphs(d.entry.Text) {
				score := matcher.score(p.Text)
				if score > 0 {
					hits = append(hits, hit{score: score, doc: d.file.doc, kind: "source", line: p.Start, end: p.End, text: p.Text, segments: []hitSegment{{doc: d.file.doc, start: p.Start, end: p.End, text: p.Text, kind: "source"}}})
				}
			}
		} else {
			for _, pg := range d.entry.Pages {
				for i, b := range pg.Blocks {
					score := matcher.score(b.Text)
					if score > 0 {
						box := b.BBox
						hits = append(hits, hit{score: score, doc: d.file.doc, kind: "pdf", page: pg.Number, line: i + 1, end: i + 1, text: b.Text, box: &box, info: pg, segments: []hitSegment{{doc: d.file.doc, page: pg.Number, start: i + 1, end: i + 1, text: b.Text, kind: "pdf"}}})
					}
				}
			}
		}
		hits = dedupHits(mergePDFHits(hits))
		for _, h := range hits {
			c := candidate{result: resultFor(d, h)}
			for _, seg := range h.segments {
				result := Result{Source: d.file.source.ID, Doc: seg.doc, Kind: seg.kind, Page: seg.page, StartLine: seg.start, EndLine: seg.end, Text: seg.text}
				c.records = append(c.records, frequencyEntry{Result: result, Digest: d.digest})
			}
			if len(c.records) > 0 {
				p := c.records[0].Result
				c.result.EntryID = entryID(p.Source, p.Doc, p.Page, p.StartLine, p.EndLine)
			}
			candidates = append(candidates, c)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i].result, candidates[j].result
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Doc != b.Doc {
			return a.Doc < b.Doc
		}
		if a.Page != b.Page {
			return a.Page < b.Page
		}
		return a.StartLine < b.StartLine
	})
	if r.Offset >= len(candidates) {
		return nil, nil, nil
	}
	end := minInt(len(candidates), r.Offset+r.Limit)
	var results []Result
	var records []frequencyEntry
	used := 0
	for _, c := range candidates[r.Offset:end] {
		remaining := r.MaxChars - used
		if remaining <= 0 {
			break
		}
		original := c.result.Text
		c.result.Text = truncate(searchSnippet(original, r.SnippetChars), minInt(remaining, r.SnippetChars))
		c.result.Truncated = len([]rune(c.result.Text)) < len([]rune(strings.TrimSpace(strings.Join(strings.Fields(original), " "))))
		used += len([]rune(c.result.Text))
		results = append(results, c.result)
		records = append(records, c.records...)
	}
	return results, records, nil
}
func (l *Library) navigate(r Request, files []sourceFile) ([]Result, error) {
	var results []Result
	scope, e := cleanDoc(r.Doc)
	if e != nil {
		return nil, e
	}
	seen := map[string]bool{}
	var pattern *regexp.Regexp
	if r.Mode == "find" && strings.ContainsAny(r.Query, "*?[") {
		query := r.Query
		if !r.CaseSensitive {
			query = strings.ToLower(query)
		}
		pattern, e = compileGlob(query)
		if e != nil {
			return nil, e
		}
	}
	for _, f := range files {
		doc := f.doc
		kind := "file"
		if r.Mode == "find" {
			lowDoc, lowQuery := doc, r.Query
			if !r.CaseSensitive {
				lowDoc = strings.ToLower(doc)
				lowQuery = strings.ToLower(r.Query)
			}
			if pattern != nil {
				if !pattern.MatchString(lowDoc) {
					continue
				}
			} else if !strings.Contains(lowDoc, lowQuery) {
				continue
			}
		}
		if r.Mode == "list" && !r.Recursive {
			relative := doc
			if scope != "." {
				relative = strings.TrimPrefix(doc, scope+"/")
			}
			if at := strings.IndexByte(relative, '/'); at >= 0 {
				kind = "directory"
				relative = relative[:at+1]
			}
			if scope != "." {
				doc = scope + "/" + relative
			} else {
				doc = relative
			}
		}
		key := f.source.ID + "\x00" + doc
		if seen[key] {
			continue
		}
		seen[key] = true
		result := Result{Source: f.source.ID, Doc: doc, Kind: kind}
		if r.Mode == "status" {
			result.Text = fmt.Sprintf("bytes=%d (not extracted)", f.size)
		}
		results = append(results, result)
	}
	return budgetResults(pageResults(results, r), r.MaxChars), nil
}
func budgetResults(results []Result, max int) []Result {
	used := 0
	out := make([]Result, 0, len(results))
	for _, r := range results {
		if used >= max {
			break
		}
		if len([]rune(r.Text)) > max-used {
			r.Text = truncate(r.Text, max-used)
			r.Truncated = true
		}
		used += len([]rune(r.Text))
		out = append(out, r)
	}
	return out
}
func (l *Library) grep(ctx context.Context, r Request, files []sourceFile, info *CacheInfo) ([]Result, []frequencyEntry, []string, error) {
	matcher, e := newMatcher(r)
	if e != nil {
		return nil, nil, nil, e
	}
	var results []Result
	var records []frequencyEntry
	var warnings []string
	matched, used := 0, 0
	for _, f := range files {
		if knownBinary(f.doc) || strings.EqualFold(filepath.Ext(f.doc), ".pdf") {
			continue
		}
		if e = ctx.Err(); e != nil {
			return nil, nil, nil, e
		}
		d, err := l.loadDocument(ctx, f, info)
		if err != nil {
			if strings.Contains(err.Error(), "binary or invalid UTF-8") {
				warnings = append(warnings, f.source.ID+":"+f.doc+": skipped binary file")
				continue
			}
			return nil, nil, nil, err
		}
		for n, line := range strings.Split(d.entry.Text, "\n") {
			if matcher.score(line) == 0 {
				continue
			}
			matched++
			if matched <= r.Offset {
				continue
			}
			if len(results) >= r.Limit || used >= r.MaxChars {
				continue
			}
			text := truncate(line, minInt(r.SnippetChars, r.MaxChars-used))
			used += len([]rune(text))
			result := Result{Source: f.source.ID, Doc: f.doc, Kind: "source", StartLine: n + 1, EndLine: n + 1, Text: text, EntryID: entryID(f.source.ID, f.doc, 0, n+1, n+1)}
			results = append(results, result)
			records = append(records, frequencyEntry{Result: result, Digest: d.digest})
		}
	}
	return results, records, warnings, nil
}
func splitPageRef(name string) (string, int, error) {
	at := strings.LastIndex(name, "#page=")
	if at < 0 {
		return name, 0, nil
	}
	n, e := strconv.Atoi(name[at+6:])
	if e != nil || n < 1 {
		return "", 0, fmt.Errorf("invalid #page selector")
	}
	return name[:at], n, nil
}
func (l *Library) locate(sources []Source, name string) (sourceFile, error) {
	doc, e := cleanDoc(name)
	if e != nil {
		return sourceFile{}, e
	}
	if doc == "." {
		return sourceFile{}, fmt.Errorf("read/view requires a document path")
	}
	var found []sourceFile
	for _, s := range sources {
		if !allowed(s, doc) {
			continue
		}
		root, e := os.OpenRoot(s.Root)
		if e != nil {
			return sourceFile{}, e
		}
		st, err := root.Stat(filepath.FromSlash(doc))
		root.Close()
		if err == nil && st.Mode().IsRegular() {
			found = append(found, sourceFile{s, doc, st.Size()})
		}
	}
	if len(found) > 1 {
		return sourceFile{}, fmt.Errorf("document %s occurs in multiple sources; select source explicitly", doc)
	}
	if len(found) == 1 {
		return found[0], nil
	}
	return sourceFile{}, fmt.Errorf("document not found or excluded by configured source policy: %s", doc)
}
func (l *Library) readOrView(ctx context.Context, r Request, sources []Source, info *CacheInfo) ([]Result, []Image, []frequencyEntry, []string, error) {
	name := r.Doc
	if name == "" {
		name = r.Query
	}
	name, ref, e := splitPageRef(name)
	if e != nil {
		return nil, nil, nil, nil, e
	}
	if ref > 0 {
		if r.Page > 0 && r.Page != ref {
			return nil, nil, nil, nil, fmt.Errorf("page argument conflicts with #page selector")
		}
		r.Page = ref
	}
	if !requestAllows(r, name) {
		return nil, nil, nil, nil, fmt.Errorf("document excluded by request kinds/extensions filter")
	}
	file, e := l.locate(sources, name)
	if e != nil {
		return nil, nil, nil, nil, e
	}
	d, e := l.loadDocument(ctx, file, info)
	if e != nil {
		return nil, nil, nil, nil, e
	}
	info.Fingerprint = d.key
	var records []frequencyEntry
	var warnings []string
	var results []Result
	var images []Image
	start := r.StartLine
	if start == 0 {
		start = 1
	}
	end := r.EndLine
	if end == 0 {
		end = start + l.config.Limits.ReadLines - 1
	}
	if end-start+1 > l.config.Limits.ReadLines {
		end = start + l.config.Limits.ReadLines - 1
		warnings = append(warnings, "read range capped by limits.read_lines")
	}
	if d.entry.Kind == "source" {
		if r.Mode == "view" {
			return nil, nil, nil, nil, fmt.Errorf("view requires a PDF document")
		}
		if r.Page > 0 {
			return nil, nil, nil, nil, fmt.Errorf("page applies only to PDF documents")
		}
		lines := strings.Split(d.entry.Text, "\n")
		end = minInt(end, len(lines))
		if start <= end {
			original := strings.Join(lines[start-1:end], "\n")
			visible := truncate(original, r.MaxChars)
			actualEnd := start + strings.Count(visible, "\n")
			if len([]rune(visible)) < len([]rune(original)) {
				end = actualEnd
			}
			result := Result{Source: file.source.ID, Doc: file.doc, Kind: "source", StartLine: start, EndLine: end, Text: visible, Truncated: len([]rune(visible)) < len([]rune(original))}
			results = append(results, result)
			for _, p := range sourceParagraphs(d.entry.Text) {
				if p.End >= start && p.Start <= end {
					records = append(records, frequencyEntry{Result: Result{Source: file.source.ID, Doc: file.doc, Kind: "source", StartLine: p.Start, EndLine: p.End, Text: p.Text}, Digest: d.digest})
				}
			}
		}
	} else {
		n := r.Page
		if n == 0 {
			n = 1
		}
		if n > len(d.entry.Pages) {
			return nil, nil, nil, nil, fmt.Errorf("page %d out of range; PDF has %d pages", n, len(d.entry.Pages))
		}
		pg := d.entry.Pages[n-1]
		if len(pg.Blocks) == 0 {
			warnings = append(warnings, "PDF page has no extractable text; no OCR is performed")
		}
		if r.Mode == "view" {
			image, e := l.render(ctx, d, pg, r)
			if e != nil {
				return nil, nil, nil, nil, e
			}
			images = append(images, image)
			region := regionFor(r)
			bb := [4]float64{region.Left * pg.Width / 100, region.Top * pg.Height / 100, region.Right * pg.Width / 100, region.Bottom * pg.Height / 100}
			pct := [4]float64{region.Left, region.Top, region.Right, region.Bottom}
			results = append(results, Result{Source: file.source.ID, Doc: file.doc, Kind: "pdf", Page: n, BBox: &bb, BBoxPct: &pct, PageWidth: pg.Width, PageHeight: pg.Height})
			for i, b := range pg.Blocks {
				if b.BBox[2] >= bb[0] && b.BBox[0] <= bb[2] && b.BBox[3] >= bb[1] && b.BBox[1] <= bb[3] {
					records = append(records, frequencyEntry{Result: Result{Source: file.source.ID, Doc: file.doc, Kind: "pdf", Page: n, StartLine: i + 1, EndLine: i + 1, Text: b.Text}, Digest: d.digest})
				}
			}
		} else {
			end = minInt(end, len(pg.Blocks))
			used := 0
			for i := start; i <= end; i++ {
				if used >= r.MaxChars {
					break
				}
				b := pg.Blocks[i-1]
				h := hit{doc: file.doc, kind: "pdf", page: n, line: i, end: i, text: b.Text, box: &b.BBox, info: pg}
				result := resultFor(d, h)
				result.Text = truncate(result.Text, r.MaxChars-used)
				result.Truncated = len([]rune(result.Text)) < len([]rune(b.Text))
				used += len([]rune(result.Text))
				results = append(results, result)
				record := result
				record.Text = b.Text
				records = append(records, frequencyEntry{Result: record, Digest: d.digest})
			}
		}
	}
	return results, images, records, warnings, nil
}
