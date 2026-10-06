package tool

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// Typed failures so callers can surface a precise, user-facing reason.
var (
	ErrUnsupportedFormat = errors.New("unsupported file format")
	ErrLegacyDoc         = errors.New("legacy .doc format is not supported")
	ErrCorrupt           = errors.New("file is corrupt or not a valid document")
	ErrNoText            = errors.New("no extractable text")
	ErrTooLarge          = errors.New("document is too large to process")
)

const (
	// MaxExtractedRunes caps the text handed to the model (~150k characters).
	MaxExtractedRunes = 150_000
	// maxDocxXMLBytes bounds the decompressed size of word/document.xml (zip-bomb guard).
	maxDocxXMLBytes = 64 << 20
	// maxPDFPages bounds work done on a single PDF.
	maxPDFPages = 500
)

// Result is the outcome of a successful extraction.
type Result struct {
	Text      string
	Truncated bool // Text was cut at MaxExtractedRunes
}

// DocumentParser parses document files and extracts text content
type DocumentParser struct{}

// NewDocumentParser creates a new DocumentParser
func NewDocumentParser() *DocumentParser {
	return &DocumentParser{}
}

// Parse extracts text from a document file. It never returns placeholder
// text: every failure mode is a typed error.
func (p *DocumentParser) Parse(filePath string) (Result, error) {
	ext := strings.ToLower(filepath.Ext(filePath))

	var (
		text string
		err  error
	)
	switch ext {
	case ".txt", ".md":
		text, err = parseText(filePath)
	case ".pdf":
		text, err = parsePDF(filePath)
	case ".docx":
		text, err = parseDocx(filePath)
	case ".doc":
		return Result{}, ErrLegacyDoc
	default:
		return Result{}, fmt.Errorf("%w: %s", ErrUnsupportedFormat, ext)
	}
	if err != nil {
		return Result{}, err
	}

	text = normalize(text)
	if strings.TrimSpace(text) == "" {
		return Result{}, ErrNoText
	}
	res := Result{Text: text}
	if r := []rune(text); len(r) > MaxExtractedRunes {
		res.Text = string(r[:MaxExtractedRunes])
		res.Truncated = true
	}
	return res, nil
}

// normalize unifies line endings, strips NULs/control noise and collapses
// runs of blank lines.
func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 0x20 && r != 0x7f && r != 0xfffd {
			return r
		}
		return -1
	}, s)
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, l := range lines {
		l = strings.TrimRight(l, " \t")
		if l == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// ---- plain text ----

func parseText(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read text file: %w", err)
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if bytes.IndexByte(data, 0) >= 0 {
		return "", fmt.Errorf("%w: binary content in text file", ErrCorrupt)
	}
	if utf8.Valid(data) {
		return string(data), nil
	}
	// Chinese legal documents are frequently saved as GBK by Windows editors.
	dec, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data)
	if err != nil {
		return "", fmt.Errorf("%w: unknown text encoding", ErrCorrupt)
	}
	return string(dec), nil
}

// ---- PDF ----

func parsePDF(filePath string) (text string, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open pdf: %w", err)
	}
	defer f.Close()

	head := make([]byte, 5)
	if _, err := io.ReadFull(f, head); err != nil || string(head) != "%PDF-" {
		return "", fmt.Errorf("%w: missing PDF header", ErrCorrupt)
	}
	st, err := f.Stat()
	if err != nil {
		return "", err
	}

	// The PDF library panics on some malformed inputs.
	defer func() {
		if r := recover(); r != nil {
			text, err = "", fmt.Errorf("%w: %v", ErrCorrupt, r)
		}
	}()

	r, err := pdf.NewReader(f, st.Size())
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	n := r.NumPage()
	if n > maxPDFPages {
		return "", fmt.Errorf("%w: %d pages (max %d)", ErrTooLarge, n, maxPDFPages)
	}

	var b strings.Builder
	for i := 1; i <= n; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		t, perr := page.GetPlainText(nil)
		if perr != nil {
			continue // one bad page should not lose the rest
		}
		b.WriteString(t)
		b.WriteString("\n")
	}
	return b.String(), nil // empty => ErrNoText upstream (scanned PDF)
}

// ---- DOCX ----

func parseDocx(filePath string) (string, error) {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	defer zr.Close()

	var doc *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc = f
			break
		}
	}
	if doc == nil {
		return "", fmt.Errorf("%w: word/document.xml not found", ErrCorrupt)
	}
	if doc.UncompressedSize64 > maxDocxXMLBytes {
		return "", fmt.Errorf("%w: document.xml too large", ErrTooLarge)
	}
	rc, err := doc.Open()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	defer rc.Close()

	return extractWordXML(io.LimitReader(rc, maxDocxXMLBytes+1))
}

// extractWordXML streams WordprocessingML and emits text with paragraph,
// table-cell and row boundaries preserved.
func extractWordXML(r io.Reader) (string, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = false

	var (
		b          strings.Builder
		inText     bool  // inside <w:t>
		fallback   int   // depth inside <mc:Fallback> (duplicate of AlternateContent/Choice)
		rowCells   []int // cells seen so far in each open table row (nested tables stack)
		cellDepth  int
		pendingSep bool // a paragraph ended inside a cell; separate it from the next text
	)
	sep := func() {
		if pendingSep {
			b.WriteByte(' ')
			pendingSep = false
		}
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if fallback > 0 {
				fallback++
				continue
			}
			switch t.Name.Local {
			case "Fallback":
				fallback = 1
			case "t":
				sep()
				inText = true
			case "tab":
				sep()
				b.WriteByte('\t')
			case "br", "cr":
				sep()
				b.WriteByte('\n')
			case "tr":
				rowCells = append(rowCells, 0)
			case "tc":
				if n := len(rowCells); n > 0 {
					if rowCells[n-1] > 0 {
						b.WriteString(" | ")
					}
					rowCells[n-1]++
				}
				cellDepth++
				pendingSep = false
			}
		case xml.EndElement:
			if fallback > 0 {
				fallback--
				continue
			}
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				if cellDepth > 0 {
					pendingSep = true
				} else {
					b.WriteByte('\n')
				}
			case "tc":
				if cellDepth > 0 {
					cellDepth--
				}
				pendingSep = false
			case "tr":
				if n := len(rowCells); n > 0 {
					rowCells = rowCells[:n-1]
				}
				b.WriteByte('\n')
			}
		case xml.CharData:
			if inText && fallback == 0 {
				b.Write(t)
			}
		}
		if b.Len() > 4*MaxExtractedRunes*utf8.UTFMax {
			return "", fmt.Errorf("%w: extracted text too large", ErrTooLarge)
		}
	}
	return b.String(), nil
}

// IsSupportedFormat reports whether the extension is accepted for upload.
// .doc is intentionally excluded: it is rejected with a dedicated message.
func IsSupportedFormat(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".pdf", ".docx", ".txt", ".md":
		return true
	}
	return false
}

// IsLegacyDoc reports whether the file is a legacy binary Word document.
func IsLegacyDoc(filename string) bool {
	return strings.EqualFold(filepath.Ext(filename), ".doc")
}
