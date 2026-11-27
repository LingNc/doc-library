package doclibrary

import (
	"encoding/json"
	"fmt"
	"strings"
)

// compactResult is the v2 wire shape. PDF block ranges remain in the Go Result
// for canonical frequency identities and precise follow-up reads, but are not
// exposed as source lines in compact JSON.
type compactResult struct {
	Source    string      `json:"source"`
	Doc       string      `json:"doc"`
	Kind      string      `json:"kind"`
	Page      int         `json:"page,omitempty"`
	BBoxPct   *[4]float64 `json:"bbox_pct,omitempty"`
	StartLine int         `json:"start_line,omitempty"`
	EndLine   int         `json:"end_line,omitempty"`
	Text      string      `json:"text,omitempty"`
	Score     float64     `json:"score,omitempty"`
	EntryID   string      `json:"entry_id,omitempty"`
	Hits      int         `json:"hits,omitempty"`
	Truncated bool        `json:"truncated,omitempty"`
}

type compactResponse struct {
	Version    string          `json:"version"`
	OK         bool            `json:"ok"`
	Mode       string          `json:"mode"`
	Results    []compactResult `json:"results"`
	Images     []Image         `json:"images"`
	Cache      *CacheInfo      `json:"cache,omitempty"`
	Warnings   []string        `json:"warnings,omitempty"`
	Truncated  bool            `json:"truncated,omitempty"`
	NextOffset *int            `json:"next_offset,omitempty"`
	Schema     map[string]any  `json:"schema,omitempty"`
}

// MarshalJSON preserves the original v1 fields exactly. V2 serializes the same
// projected Results without the Go-only formatted Text mirror, absolute PDF
// geometry, or routine cache diagnostics. Hosts can still use Response.Text and
// Response.Cache directly. No second persistent body representation is kept.
func (r Response) MarshalJSON() ([]byte, error) {
	if r.Version != CompactVersion {
		type legacyResponse Response
		return json.Marshal(legacyResponse(r))
	}
	wire := compactResponse{Version: r.Version, OK: r.OK, Mode: r.Mode, Images: r.Images,
		Warnings: compactWarnings(r.Warnings), Truncated: r.Truncated, NextOffset: r.NextOffset, Schema: r.Schema}
	if r.Mode == "status" || r.Mode == "index" {
		wire.Cache = &r.Cache
	}
	if r.Results != nil {
		wire.Results = make([]compactResult, 0, len(r.Results))
	}
	for _, result := range r.Results {
		start, end := result.StartLine, result.EndLine
		if result.Kind == "pdf" {
			start, end = 0, 0
		}
		wire.Results = append(wire.Results, compactResult{
			Source: result.Source, Doc: result.Doc, Kind: result.Kind, Page: result.Page,
			BBoxPct: result.BBoxPct, StartLine: start, EndLine: end, Text: result.Text,
			Score: result.Score, EntryID: result.EntryID, Hits: result.Hits, Truncated: result.Truncated,
		})
	}
	return json.Marshal(wire)
}

func formatCompactResults(results []Result) string {
	rows := make([]string, 0, len(results))
	for _, r := range results {
		where := r.Source + " · " + r.Doc
		if r.Page > 0 {
			where += fmt.Sprintf(" · p.%d", r.Page)
		} else if r.StartLine > 0 {
			where += fmt.Sprintf(" · L%d–%d", r.StartLine, r.EndLine)
		}
		if r.BBoxPct != nil {
			b := *r.BBoxPct
			where += fmt.Sprintf("\nbbox_pct=[%.2f,%.2f,%.2f,%.2f]", b[0], b[1], b[2], b[3])
		}
		if r.Text != "" {
			where += "\n" + r.Text
		}
		rows = append(rows, where)
	}
	return strings.Join(rows, "\n\n")
}

func compactWarnings(warnings []string) []string {
	var out []string
	for _, warning := range warnings {
		// Only known routine binary skips are diagnostic. Extraction failures,
		// no-OCR notices, invalid caches, pagination and truncation remain visible.
		if strings.HasSuffix(warning, ": binary or invalid UTF-8 skipped") || strings.HasSuffix(warning, ": skipped binary file") {
			continue
		}
		out = append(out, warning)
	}
	return out
}

// projectResults runs only after rendering and frequency recording, so hiding
// output geometry cannot affect automatic cropping or persistent history.
func projectResults(results []Result, version int, includeBBox bool) {
	for i := range results {
		r := &results[i]
		if !includeBBox || version == 2 {
			r.BBox = nil
			r.PageWidth, r.PageHeight = 0, 0
		}
		if !includeBBox {
			r.BBoxPct = nil
		}
	}
}
