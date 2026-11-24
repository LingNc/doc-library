// Derived from compare-figure go/luadraw/docs.go and doc_files.go.
// Source revision 820376cdc4158f8df7f3fcc61f678032f11bc6b9.
// Copyright (c) 2026 LingNc. MIT; see LICENSE and docs provenance.
package doclibrary

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type bbox = [4]float64
type block struct {
	Text string `json:"text"`
	BBox bbox   `json:"bbox"`
}
type page struct {
	Number int     `json:"page"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Blocks []block `json:"blocks"`
}
type docEntry struct {
	Path  string `json:"path"`
	Kind  string `json:"kind"`
	Text  string `json:"text,omitempty"`
	Pages []page `json:"pages,omitempty"`
}

type sourceParagraph struct {
	Start int
	End   int
	Text  string
}

func sourceParagraphs(text string) []sourceParagraph {
	lines := strings.Split(text, "\n")
	const maxParagraphLines = 80
	const maxParagraphBytes = 12000
	var out []sourceParagraph
	for i := 0; i < len(lines); {
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			i++
		}
		if i >= len(lines) {
			break
		}
		start := i
		size := 0
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
			lineSize := len(lines[i]) + 1
			if i > start && (i-start >= maxParagraphLines || size+lineSize > maxParagraphBytes) {
				break
			}
			size += lineSize
			i++
		}
		out = append(out, sourceParagraph{Start: start + 1, End: i, Text: strings.Join(lines[start:i], "\n")})
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func min3(a, b, c int) int { return minInt(minInt(a, b), c) }

func editDistance(a, b string) int {
	x, y := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	prev := make([]int, len(y)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, ca := range x {
		curr := make([]int, len(y)+1)
		curr[0] = i + 1
		for j, cb := range y {
			cost := 0
			if ca != cb {
				cost = 1
			}
			curr[j+1] = min3(curr[j]+1, prev[j+1]+1, prev[j]+cost)
		}
		prev = curr
	}
	return prev[len(y)]
}

func commonPrefixRunes(a, b string) int {
	x, y := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	n := minInt(len(x), len(y))
	for i := 0; i < n; i++ {
		if x[i] != y[i] {
			return i
		}
	}
	return n
}

func pdfParagraphs(lines []block, pageWidth float64) []block {
	if len(lines) == 0 {
		return nil
	}
	if pageWidth <= 0 {
		for _, line := range lines {
			if line.BBox[2] > pageWidth {
				pageWidth = line.BBox[2]
			}
		}
	}
	starts := make([]float64, len(lines))
	for i := range lines {
		starts[i] = lines[i].BBox[0]
	}
	sort.Float64s(starts)
	unique := starts[:0]
	for _, x := range starts {
		if len(unique) == 0 || x != unique[len(unique)-1] {
			unique = append(unique, x)
		}
	}
	var cuts []float64
	threshold := pageWidth * .12
	for i := 1; i < len(unique); i++ {
		if unique[i]-unique[i-1] > threshold {
			cuts = append(cuts, (unique[i]+unique[i-1])/2)
		}
	}
	column := func(x float64) int {
		col := 0
		for _, cut := range cuts {
			if x > cut {
				col++
			}
		}
		return col
	}
	ordered := append([]block(nil), lines...)
	sort.SliceStable(ordered, func(i, j int) bool {
		ci, cj := column(ordered[i].BBox[0]), column(ordered[j].BBox[0])
		if ci != cj {
			return ci < cj
		}
		if ordered[i].BBox[1] != ordered[j].BBox[1] {
			return ordered[i].BBox[1] < ordered[j].BBox[1]
		}
		return ordered[i].BBox[0] < ordered[j].BBox[0]
	})
	out := make([]block, 0, len(ordered))
	current := ordered[0]
	lastLineHeight := maxF(1, current.BBox[3]-current.BBox[1])
	lineCount := 1
	for _, next := range ordered[1:] {
		gap := next.BBox[1] - current.BBox[3]
		if gap < 0 {
			gap = 0
		}
		nextLineHeight := maxF(1, next.BBox[3]-next.BBox[1])
		minWidth := minF(current.BBox[2]-current.BBox[0], next.BBox[2]-next.BBox[0])
		horizontal := overlap1(current.BBox[0], current.BBox[2], next.BBox[0], next.BBox[2])
		joins := gap <= maxF(1.6*maxF(lastLineHeight, nextLineHeight), 3) && horizontal >= maxF(1, minWidth*.18) && lineCount < 12 && len([]rune(current.Text))+len([]rune(next.Text))+1 <= 2048 && !pdfHeadingBoundary(next.Text)
		if !joins {
			out = append(out, current)
			current = next
			lastLineHeight = nextLineHeight
			lineCount = 1
			continue
		}
		current.Text += "\n" + next.Text
		if next.BBox[0] < current.BBox[0] {
			current.BBox[0] = next.BBox[0]
		}
		if next.BBox[1] < current.BBox[1] {
			current.BBox[1] = next.BBox[1]
		}
		if next.BBox[2] > current.BBox[2] {
			current.BBox[2] = next.BBox[2]
		}
		if next.BBox[3] > current.BBox[3] {
			current.BBox[3] = next.BBox[3]
		}
		lastLineHeight = nextLineHeight
		lineCount++
	}
	out = append(out, current)
	return out
}

var pdfNumberedHeading = regexp.MustCompile(`^\d+(?:\.\d+)*[.)]?\s+\S`)

func pdfHeadingBoundary(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return true
	}
	if pdfNumberedHeading.MatchString(text) {
		return true
	}
	lower := strings.ToLower(text)
	for _, marker := range []string{"new section", "section ", "chapter ", "part "} {
		if strings.HasPrefix(lower, marker) {
			return true
		}
	}
	return false
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type hitSegment struct {
	doc   string
	page  int
	start int
	end   int
	text  string
	kind  string
}

type hit struct {
	score           int
	doc             string
	page, line, end int
	kind, text      string
	box             *bbox
	info            page
	segments        []hitSegment
}

func searchSnippet(s string, max int) string {
	if max <= 0 {
		max = 1200
	}
	if max < 64 {
		max = 64
	}
	if max > 8000 {
		max = 8000
	}
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	if len([]rune(s)) <= max {
		return s
	}
	cut := []rune(s)[:max]
	end := max
	for i := max - 1; i >= max/2; i-- {
		if cut[i] == ' ' || cut[i] == '.' || cut[i] == ';' || cut[i] == ',' {
			end = i
			break
		}
	}
	if end < 64 {
		end = max
	}
	return strings.TrimSpace(string(cut[:end])) + "…"
}

func normalizeHitText(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

func overlap1(a1, a2, b1, b2 float64) float64 {
	x := minF(a2, b2) - maxF(a1, b1)
	if x < 0 {
		return 0
	}
	return x
}
func mergePDFHits(hits []hit) []hit {
	used := make([]bool, len(hits))
	out := make([]hit, 0, len(hits))
	for i, h := range hits {
		if used[i] || h.kind != "pdf" || h.box == nil {
			if !used[i] {
				out = append(out, h)
				used[i] = true
			}
			continue
		}
		used[i] = true
		merged := h
		if len(merged.segments) == 0 {
			merged.segments = []hitSegment{{doc: h.doc, page: h.page, start: h.line, end: maxInt(h.line, h.end), text: h.text, kind: h.kind}}
		}
		box := *h.box
		texts := []string{h.text}
		primary := h.text
		primaryScore := h.score
		changed := true
		for changed {
			changed = false
			for j, c := range hits {
				if used[j] || c.kind != "pdf" || c.doc != h.doc || c.page != h.page || c.box == nil {
					continue
				}
				gap := 0.0
				if c.box[1] > box[3] {
					gap = c.box[1] - box[3]
				} else if box[1] > c.box[3] {
					gap = box[1] - c.box[3]
				}
				lineH := maxF(box[3]-box[1], c.box[3]-c.box[1])
				horiz := overlap1(box[0], box[2], c.box[0], c.box[2])
				minW := minF(box[2]-box[0], c.box[2]-c.box[0])
				if gap <= maxF(2*lineH, 12) && horiz >= maxF(1, minW*.15) {
					used[j] = true
					if c.box[0] < box[0] {
						box[0] = c.box[0]
					}
					if c.box[1] < box[1] {
						box[1] = c.box[1]
					}
					if c.box[2] > box[2] {
						box[2] = c.box[2]
					}
					if c.box[3] > box[3] {
						box[3] = c.box[3]
					}
					if len(c.segments) == 0 {
						c.segments = []hitSegment{{doc: c.doc, page: c.page, start: c.line, end: maxInt(c.line, c.end), text: c.text, kind: c.kind}}
					}
					merged.segments = append(merged.segments, c.segments...)
					if merged.line == 0 || c.line > 0 && c.line < merged.line {
						merged.line = c.line
					}
					if c.end > merged.end {
						merged.end = c.end
					}
					texts = append(texts, c.text)
					if c.score > primaryScore {
						primary = c.text
						primaryScore = c.score
					}
					if c.score > merged.score {
						merged.score = c.score
					}
					changed = true
				}
			}
		}
		merged.box = &box
		if len(texts) > 0 && texts[0] != primary {
			rest := make([]string, 0, len(texts))
			rest = append(rest, primary)
			for _, t := range texts {
				if t != primary {
					rest = append(rest, t)
				}
			}
			texts = rest
		}
		merged.text = strings.Join(texts, " ")
		out = append(out, merged)
	}
	return out
}
func dedupHits(hits []hit) []hit {
	seen := map[string]bool{}
	out := make([]hit, 0, len(hits))
	for _, h := range hits {
		key := fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d", h.doc, h.kind, h.page, h.line, maxInt(h.line, h.end))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, h)
	}
	return out
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
