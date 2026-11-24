// Package doclibrary indexes configured local document collections.
// Paragraph/navigation/PDF algorithms derive from compare-figure go/luadraw
// at 820376cdc4158f8df7f3fcc61f678032f11bc6b9 (MIT, 2026 LingNc).
package doclibrary

import "context"

const Version = "doc-library/1"

type Config struct {
	Sources   []Source          `json:"sources" yaml:"sources"`
	CacheDir  string            `json:"cache_dir,omitempty" yaml:"cache_dir,omitempty"`
	CacheMode string            `json:"cache_mode,omitempty" yaml:"cache_mode,omitempty"`
	Search    SearchSettings    `json:"search,omitempty" yaml:"search,omitempty"`
	Limits    Limits            `json:"limits,omitempty" yaml:"limits,omitempty"`
	Frequency FrequencySettings `json:"frequency,omitempty" yaml:"frequency,omitempty"`
	PDF       PDFSettings       `json:"pdf,omitempty" yaml:"pdf,omitempty"`
}
type Source struct {
	ID         string   `json:"id" yaml:"id"`
	Root       string   `json:"root" yaml:"root"`
	Include    []string `json:"include,omitempty" yaml:"include,omitempty"`
	Exclude    []string `json:"exclude,omitempty" yaml:"exclude,omitempty"`
	Extensions []string `json:"extensions,omitempty" yaml:"extensions,omitempty"`
	Weight     float64  `json:"weight,omitempty" yaml:"weight,omitempty"`
}
type SearchSettings struct {
	Match        string `json:"match,omitempty" yaml:"match,omitempty"`
	Limit        int    `json:"limit,omitempty" yaml:"limit,omitempty"`
	SnippetChars int    `json:"snippet_chars,omitempty" yaml:"snippet_chars,omitempty"`
	MaxChars     int    `json:"max_chars,omitempty" yaml:"max_chars,omitempty"`
}
type Limits struct {
	MaxFileBytes  int64 `json:"max_file_bytes,omitempty" yaml:"max_file_bytes,omitempty"`
	MaxTotalBytes int64 `json:"max_total_bytes,omitempty" yaml:"max_total_bytes,omitempty"`
	MaxFiles      int   `json:"max_files,omitempty" yaml:"max_files,omitempty"`
	MaxResults    int   `json:"max_results,omitempty" yaml:"max_results,omitempty"`
	ReadLines     int   `json:"read_lines,omitempty" yaml:"read_lines,omitempty"`
}
type FrequencySettings struct {
	Enabled    *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	MaxEntries int   `json:"max_entries,omitempty" yaml:"max_entries,omitempty"`
	TextChars  int   `json:"text_chars,omitempty" yaml:"text_chars,omitempty"`
}
type PDFSettings struct {
	PDFToText      string `json:"pdftotext,omitempty" yaml:"pdftotext,omitempty"`
	PDFToPPM       string `json:"pdftoppm,omitempty" yaml:"pdftoppm,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" yaml:"timeout_seconds,omitempty"`
}

// Region is a percentage rectangle, with origin at the upper left.
type Region struct {
	Left   float64 `json:"left" yaml:"left"`
	Top    float64 `json:"top" yaml:"top"`
	Right  float64 `json:"right" yaml:"right"`
	Bottom float64 `json:"bottom" yaml:"bottom"`
}
type Request struct {
	Mode          string   `json:"mode,omitempty" yaml:"mode,omitempty"`
	Source        string   `json:"source,omitempty" yaml:"source,omitempty"`
	Sources       []string `json:"sources,omitempty" yaml:"sources,omitempty"`
	Doc           string   `json:"doc,omitempty" yaml:"doc,omitempty"`
	Query         string   `json:"query,omitempty" yaml:"query,omitempty"`
	Kinds         []string `json:"kinds,omitempty" yaml:"kinds,omitempty"`
	Extensions    []string `json:"extensions,omitempty" yaml:"extensions,omitempty"`
	Regex         bool     `json:"regex,omitempty" yaml:"regex,omitempty"`
	CaseSensitive bool     `json:"case_sensitive,omitempty" yaml:"case_sensitive,omitempty"`
	Match         string   `json:"match,omitempty" yaml:"match,omitempty"`
	Limit         int      `json:"limit,omitempty" yaml:"limit,omitempty"`
	Offset        int      `json:"offset,omitempty" yaml:"offset,omitempty"`
	SnippetChars  int      `json:"snippet_chars,omitempty" yaml:"snippet_chars,omitempty"`
	MaxChars      int      `json:"max_chars,omitempty" yaml:"max_chars,omitempty"`
	LineChars     int      `json:"line_chars,omitempty" yaml:"line_chars,omitempty"`
	StartLine     int      `json:"start_line,omitempty" yaml:"start_line,omitempty"`
	EndLine       int      `json:"end_line,omitempty" yaml:"end_line,omitempty"`
	Page          int      `json:"page,omitempty" yaml:"page,omitempty"`
	Region        *Region  `json:"region,omitempty" yaml:"region,omitempty"`
	OutDir        string   `json:"out_dir,omitempty" yaml:"out_dir,omitempty"`
	DPI           int      `json:"dpi,omitempty" yaml:"dpi,omitempty"`
	Zoom          int      `json:"zoom,omitempty" yaml:"zoom,omitempty"`
	Recursive     bool     `json:"recursive,omitempty" yaml:"recursive,omitempty"`
	IncludeImage  bool     `json:"include_image,omitempty" yaml:"include_image,omitempty"`
	InlineImages  bool     `json:"inline_images,omitempty" yaml:"inline_images,omitempty"`
	EntryID       string   `json:"entry_id,omitempty" yaml:"entry_id,omitempty"`
	Passive       bool     `json:"passive,omitempty" yaml:"passive,omitempty"`
}
type Result struct {
	Source     string      `json:"source"`
	Doc        string      `json:"doc"`
	Kind       string      `json:"kind"`
	Page       int         `json:"page,omitempty"`
	BBox       *[4]float64 `json:"bbox,omitempty"`
	BBoxPct    *[4]float64 `json:"bbox_pct,omitempty"`
	PageWidth  float64     `json:"page_width,omitempty"`
	PageHeight float64     `json:"page_height,omitempty"`
	StartLine  int         `json:"start_line,omitempty"`
	EndLine    int         `json:"end_line,omitempty"`
	Text       string      `json:"text,omitempty"`
	Score      float64     `json:"score,omitempty"`
	EntryID    string      `json:"entry_id,omitempty"`
	Hits       int         `json:"hits,omitempty"`
	Truncated  bool        `json:"truncated,omitempty"`
}
type Image struct {
	Path   string `json:"path"`
	MIME   string `json:"mime"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Base64 string `json:"base64,omitempty"`
}
type CacheInfo struct {
	Namespace   string `json:"namespace,omitempty"`
	Hits        int    `json:"hits"`
	Misses      int    `json:"misses"`
	Files       int    `json:"files"`
	Fingerprint string `json:"fingerprint,omitempty"`
}
type Response struct {
	Version    string         `json:"version"`
	OK         bool           `json:"ok"`
	Mode       string         `json:"mode"`
	Text       string         `json:"text,omitempty"`
	Results    []Result       `json:"results"`
	Images     []Image        `json:"images"`
	Cache      CacheInfo      `json:"cache"`
	Warnings   []string       `json:"warnings,omitempty"`
	Truncated  bool           `json:"truncated,omitempty"`
	NextOffset *int           `json:"next_offset,omitempty"`
	Schema     map[string]any `json:"schema,omitempty"`
}
type Executor interface {
	Execute(context.Context, Request) (Response, error)
}
