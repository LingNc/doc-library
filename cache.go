package doclibrary

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type cachedDocument struct {
	Schema    string   `json:"schema"`
	SHA       string   `json:"sha256"`
	Extractor string   `json:"extractor"`
	Entry     docEntry `json:"entry"`
}
type document struct {
	file   sourceFile
	entry  docEntry
	digest string
	key    string
}

func (l *Library) loadDocument(ctx context.Context, f sourceFile, info *CacheInfo) (document, error) {
	data, e := l.readFile(f.source, f.doc)
	if e != nil {
		return document{}, e
	}
	digest := hashBytes(data)
	info.Files++
	kind := "source"
	tool := "utf8"
	if strings.EqualFold(filepath.Ext(f.doc), ".pdf") {
		kind = "pdf"
		tool = l.config.PDF.PDFToText
		if p, err := exec.LookPath(tool); err == nil {
			if st, err := os.Stat(p); err == nil {
				tool = fmt.Sprintf("%s:%d:%d", p, st.Size(), st.ModTime().UnixNano())
			}
		}
	} else if !isText(data) {
		return document{}, fmt.Errorf("document is binary or invalid UTF-8: %s", f.doc)
	}
	key := hashBytes([]byte(extractionSchema + "\x00" + f.source.ID + "\x00" + f.doc + "\x00" + digest + "\x00" + tool))
	path := l.cachePath("files", key+".json")
	result := document{file: f, digest: digest, key: key}
	readCache := func() bool {
		b, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		var c cachedDocument
		if json.Unmarshal(b, &c) != nil || c.Schema != extractionSchema || c.SHA != digest || c.Extractor != tool || c.Entry.Path != f.doc || c.Entry.Kind != kind {
			return false
		}
		result.entry = c.Entry
		return true
	}
	if l.config.CacheMode != "off" && readCache() {
		info.Hits++
		return result, nil
	}
	var release func()
	if l.config.CacheMode != "off" {
		release, e = acquireLock(ctx, path+".lock")
		if e != nil {
			return document{}, e
		}
		defer release()
		if readCache() {
			info.Hits++
			return result, nil
		}
	}
	info.Misses++
	entry := docEntry{Path: f.doc, Kind: kind}
	if kind == "pdf" {
		entry.Pages, e = l.extractPDF(ctx, data)
		if e != nil {
			return document{}, fmt.Errorf("source %s PDF %s: %w", f.source.ID, f.doc, e)
		}
	} else {
		entry.Text = string(data)
	}
	if l.config.CacheMode != "off" {
		if e = atomicJSON(path, cachedDocument{extractionSchema, digest, tool, entry}); e != nil {
			return document{}, e
		}
	}
	result.entry = entry
	return result, nil
}

func knownBinary(doc string) bool {
	switch strings.ToLower(filepath.Ext(doc)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".ico", ".tif", ".tiff", ".zip", ".gz", ".tar", ".xz", ".7z", ".woff", ".woff2", ".ttf", ".otf", ".mp4", ".mp3", ".wav", ".exe", ".dll", ".so", ".a", ".o", ".class", ".pyc", ".docx", ".xlsx", ".pptx":
		return true
	}
	return false
}
func fingerprintDocuments(ds []document) string {
	var b strings.Builder
	for _, d := range ds {
		b.WriteString(d.file.source.ID)
		b.WriteByte(0)
		b.WriteString(d.file.doc)
		b.WriteByte(0)
		b.WriteString(d.key)
		b.WriteByte(0)
	}
	return hashBytes([]byte(b.String()))
}
func (l *Library) loadAll(ctx context.Context, files []sourceFile, info *CacheInfo) ([]document, []string, error) {
	var ds []document
	var warnings []string
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if knownBinary(f.doc) {
			continue
		}
		d, e := l.loadDocument(ctx, f, info)
		if e != nil {
			if strings.Contains(e.Error(), "binary or invalid UTF-8") {
				warnings = append(warnings, f.source.ID+":"+f.doc+": binary or invalid UTF-8 skipped")
				continue
			}
			return nil, nil, e
		}
		ds = append(ds, d)
	}
	info.Fingerprint = fingerprintDocuments(ds)
	return ds, warnings, nil
}
