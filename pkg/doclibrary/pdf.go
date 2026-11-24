// PDF extraction and paragraph geometry derive from compare-figure docs.go.
// Poppler's flow/block/line hierarchy is retained before the existing paragraph
// merger, preventing same-height words from opposite columns being combined.
package doclibrary

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/image/draw"
)

type bboxWord struct {
	X1   float64 `xml:"xMin,attr"`
	Y1   float64 `xml:"yMin,attr"`
	X2   float64 `xml:"xMax,attr"`
	Y2   float64 `xml:"yMax,attr"`
	Text string  `xml:",chardata"`
}
type bboxLine struct {
	Words []bboxWord `xml:"word"`
}
type bboxBlock struct {
	Lines []bboxLine `xml:"line"`
}
type bboxFlow struct {
	Blocks []bboxBlock `xml:"block"`
}
type bboxPage struct {
	Width  float64    `xml:"width,attr"`
	Height float64    `xml:"height,attr"`
	Flows  []bboxFlow `xml:"flow"`
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}
func joinPDFWords(words []bboxWord) (string, bbox) {
	var b strings.Builder
	var box bbox
	var previous rune
	first := true
	for _, w := range words {
		s := strings.TrimSpace(w.Text)
		if s == "" {
			continue
		}
		rs := []rune(s)
		if !first && !(isCJK(previous) && isCJK(rs[0])) {
			b.WriteByte(' ')
		}
		b.WriteString(s)
		previous = rs[len(rs)-1]
		if first {
			box = bbox{w.X1, w.Y1, w.X2, w.Y2}
			first = false
		} else {
			box[0] = minF(box[0], w.X1)
			box[1] = minF(box[1], w.Y1)
			box[2] = maxF(box[2], w.X2)
			box[3] = maxF(box[3], w.Y2)
		}
	}
	return b.String(), box
}
func parsePDFXML(r io.Reader) ([]page, error) {
	decoder := xml.NewDecoder(r)
	var pages []page
	for {
		token, e := decoder.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "page" {
			continue
		}
		var raw bboxPage
		if e = decoder.DecodeElement(&raw, &start); e != nil {
			return nil, e
		}
		if raw.Width <= 0 || raw.Height <= 0 {
			return nil, fmt.Errorf("PDF has invalid page dimensions")
		}
		p := page{Number: len(pages) + 1, Width: raw.Width, Height: raw.Height}
		for _, flow := range raw.Flows {
			for _, b := range flow.Blocks {
				var lines []block
				for _, line := range b.Lines {
					text, box := joinPDFWords(line.Words)
					if text != "" {
						lines = append(lines, block{text, box})
					}
				}
				p.Blocks = append(p.Blocks, pdfParagraphs(lines, p.Width)...)
			}
		}
		pages = append(pages, p)
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("pdftotext returned no pages")
	}
	return pages, nil
}
func toolPath(name string) (string, error) {
	p, e := exec.LookPath(name)
	if e != nil {
		return "", fmt.Errorf("required PDF executable %q not found; install Poppler or configure its executable path", name)
	}
	return p, nil
}
func (l *Library) extractPDF(ctx context.Context, data []byte) ([]page, error) {
	tool, e := toolPath(l.config.PDF.PDFToText)
	if e != nil {
		return nil, e
	}
	// A private byte snapshot binds extracted text to the SHA, even when another
	// application replaces the source PDF while this request runs.
	base := ""
	if l.config.CacheMode != "off" {
		base = l.cachePath("tmp")
		if e = os.MkdirAll(base, 0700); e != nil {
			return nil, e
		}
	}
	f, e := os.CreateTemp(base, "doc-library-pdf-*.pdf")
	if e != nil {
		return nil, e
	}
	path := f.Name()
	defer os.Remove(path)
	if _, e = f.Write(data); e != nil {
		f.Close()
		return nil, e
	}
	if e = f.Close(); e != nil {
		return nil, e
	}
	sub, cancel := context.WithTimeout(ctx, time.Duration(l.config.PDF.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(sub, tool, "-bbox-layout", "-enc", "UTF-8", path, "-")
	b, e := cmd.Output()
	if e != nil {
		if sub.Err() != nil {
			return nil, sub.Err()
		}
		if x, ok := e.(*exec.ExitError); ok {
			return nil, fmt.Errorf("pdftotext: %s: %w", truncate(string(x.Stderr), 3000), e)
		}
		return nil, e
	}
	return parsePDFXML(strings.NewReader(string(b)))
}
func regionFor(r Request) Region {
	if r.Region != nil {
		return *r.Region
	}
	return Region{Right: 100, Bottom: 100}
}
func (l *Library) render(ctx context.Context, d document, pg page, r Request) (Image, error) {
	tool, e := toolPath(l.config.PDF.PDFToPPM)
	if e != nil {
		return Image{}, e
	}
	out, e := l.outputDir(r.OutDir)
	if e != nil {
		return Image{}, e
	}
	data, e := l.readFile(d.file.source, d.file.doc)
	if e != nil {
		return Image{}, e
	}
	if hashBytes(data) != d.digest {
		return Image{}, fmt.Errorf("PDF changed during request; retry")
	}
	input, e := os.CreateTemp(out, ".snapshot-*.pdf")
	if e != nil {
		return Image{}, e
	}
	inputName := input.Name()
	defer os.Remove(inputName)
	if _, e = input.Write(data); e != nil {
		input.Close()
		return Image{}, e
	}
	if e = input.Close(); e != nil {
		return Image{}, e
	}
	prefix := filepath.Join(out, "page")
	args := []string{"-singlefile", "-f", strconv.Itoa(pg.Number), "-l", strconv.Itoa(pg.Number), "-png"}
	if r.DPI > 0 {
		args = append(args, "-r", strconv.Itoa(r.DPI))
	} else {
		zoom := r.Zoom
		if zoom == 0 {
			zoom = 1200
		}
		args = append(args, "-scale-to-x", strconv.Itoa(zoom), "-scale-to-y", "-1")
	}
	args = append(args, inputName, prefix)
	sub, cancel := context.WithTimeout(ctx, time.Duration(l.config.PDF.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(sub, tool, args...)
	if b, e := cmd.CombinedOutput(); e != nil {
		if sub.Err() != nil {
			return Image{}, sub.Err()
		}
		return Image{}, fmt.Errorf("pdftoppm: %s: %w", truncate(string(b), 3000), e)
	}
	raster := prefix + ".png"
	defer os.Remove(raster)
	f, e := os.Open(raster)
	if e != nil {
		return Image{}, e
	}
	im, e := png.Decode(f)
	f.Close()
	if e != nil {
		return Image{}, e
	}
	bounds := im.Bounds()
	region := regionFor(r)
	rect := image.Rect(int(float64(bounds.Dx())*region.Left/100), int(float64(bounds.Dy())*region.Top/100), int(float64(bounds.Dx())*region.Right/100), int(float64(bounds.Dy())*region.Bottom/100)).Intersect(bounds)
	if rect.Empty() {
		return Image{}, fmt.Errorf("PDF crop is empty")
	}
	cropped := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(cropped, cropped.Bounds(), im, rect.Min, draw.Src)
	var result image.Image = cropped
	if r.Zoom > 0 && r.Zoom != rect.Dx() {
		dst := image.NewRGBA(image.Rect(0, 0, r.Zoom, maxInt(1, int(float64(rect.Dy())*float64(r.Zoom)/float64(rect.Dx())))))
		draw.CatmullRom.Scale(dst, dst.Bounds(), cropped, cropped.Bounds(), draw.Over, nil)
		result = dst
	}
	target := filepath.Join(out, "document.png")
	f, e = os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return Image{}, e
	}
	e = png.Encode(f, result)
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return Image{}, e
	}
	artifact := Image{Path: target, MIME: "image/png", Width: result.Bounds().Dx(), Height: result.Bounds().Dy()}
	if r.InlineImages {
		b, e := os.ReadFile(target)
		if e != nil {
			return Image{}, e
		}
		artifact.Base64 = base64.StdEncoding.EncodeToString(b)
	}
	return artifact, nil
}
