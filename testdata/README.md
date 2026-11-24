# Integration fixtures

`integration_test.go` creates all fixtures in `t.TempDir()`; no user manuals, repository documents, or generated PDFs are copied here.

The PDF integration fixture is written by the test using only the Go standard library. It is an ASCII PDF 1.4 with one page, Helvetica text, and two columns. Each column has two paragraphs positioned on matching baselines so the PDF text path can be checked for false cross-column joins. Tests that need PDF extraction require Poppler `pdftotext`; only those tests skip when it is unavailable. The crop test separately requires `pdftoppm`.

Text fixtures include Chinese mixed with Latin terms to exercise default/all/any/phrase matching without external dependencies.
