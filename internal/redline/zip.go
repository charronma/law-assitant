package redline

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
)

const (
	maxPartBytes = 64 << 20

	relTypeComments = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments"
	ctComments      = "application/vnd.openxmlformats-officedocument.wordprocessingml.comments+xml"
)

// docxFiles are the parts of a .docx this package reads or changes.
type docxFiles struct {
	zr          *zip.Reader
	document    []byte
	rels        []byte // word/_rels/document.xml.rels (may be nil)
	contentType []byte // [Content_Types].xml
	comments    []byte // word/comments.xml (nil when absent)
}

func readPart(f *zip.File) ([]byte, error) {
	if f.UncompressedSize64 > maxPartBytes {
		return nil, fmt.Errorf("%w: %s is too large", ErrUnsupported, f.Name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, maxPartBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxPartBytes {
		return nil, fmt.Errorf("%w: %s is too large", ErrUnsupported, f.Name)
	}
	return b, nil
}

func readZip(docx []byte) (*docxFiles, error) {
	zr, err := zip.NewReader(bytes.NewReader(docx), int64(len(docx)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	out := &docxFiles{zr: zr}
	for _, f := range zr.File {
		var dst *[]byte
		switch f.Name {
		case "word/document.xml":
			dst = &out.document
		case "word/_rels/document.xml.rels":
			dst = &out.rels
		case "[Content_Types].xml":
			dst = &out.contentType
		case "word/comments.xml":
			dst = &out.comments
		default:
			continue
		}
		b, err := readPart(f)
		if err != nil {
			return nil, err
		}
		*dst = b
	}
	if out.document == nil {
		return nil, fmt.Errorf("%w: word/document.xml not found", ErrUnsupported)
	}
	return out, nil
}

// write emits a copy of the package in which the given parts are replaced (or
// added, appended at the end) and every other part is copied untouched.
func (d *docxFiles) write(replace map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	done := map[string]bool{}
	for _, f := range d.zr.File {
		if data, ok := replace[f.Name]; ok {
			w, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: f.Modified})
			if err != nil {
				return nil, err
			}
			if _, err := w.Write(data); err != nil {
				return nil, err
			}
			done[f.Name] = true
			continue
		}
		if err := zw.Copy(f); err != nil {
			return nil, err
		}
	}
	for name, data := range replace {
		if done[name] {
			continue
		}
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// withCommentsPart registers word/comments.xml in the rels and content types
// when the package does not have it yet.
func (d *docxFiles) withCommentsPart(replace map[string][]byte) error {
	if d.comments == nil {
		if d.contentType == nil || d.rels == nil {
			return fmt.Errorf("%w: package lacks content types or relationships", ErrUnsupported)
		}
		ct := string(d.contentType)
		if !strings.Contains(ct, "/word/comments.xml") {
			i := strings.LastIndex(ct, "</Types>")
			if i < 0 {
				return fmt.Errorf("%w: malformed [Content_Types].xml", ErrUnsupported)
			}
			ct = ct[:i] + `<Override PartName="/word/comments.xml" ContentType="` + ctComments + `"/>` + ct[i:]
			replace["[Content_Types].xml"] = []byte(ct)
		}
		rels := string(d.rels)
		if !strings.Contains(rels, relTypeComments) {
			i := strings.LastIndex(rels, "</Relationships>")
			if i < 0 {
				return fmt.Errorf("%w: malformed document relationships", ErrUnsupported)
			}
			rels = rels[:i] + `<Relationship Id="rIdAIComments" Type="` + relTypeComments + `" Target="comments.xml"/>` + rels[i:]
			replace["word/_rels/document.xml.rels"] = []byte(rels)
		}
	}
	return nil
}
