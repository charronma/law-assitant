// Package redline applies an AI-proposed revision plan to an existing .docx as
// Word tracked changes (w:ins / w:del) and comments, leaving everything it does
// not touch byte-for-byte identical (formatting, styles, numbering, the
// document's existing revisions).
//
// It never rewrites the document through an object model: it parses
// word/document.xml only to learn byte ranges, then splices new XML into those
// ranges. Every edit is verified before it is kept (see Apply).
package redline

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	nsW  = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsMC = "http://schemas.openxmlformats.org/markup-compatibility/2006"
)

// ErrUnsupported means the document uses a structure this package cannot edit safely.
var ErrUnsupported = errors.New("redline: unsupported document structure")

// node is an element of document.xml with the byte range it occupies.
type node struct {
	space, name string
	start       int // offset of '<'
	openEnd     int // offset just past the start tag's '>'
	end         int // offset just past the end tag
	parent      *node
	kids        []*node
	attrs       map[string]string // attribute local name -> value
	text        string            // character data of w:t / w:delText
}

func (n *node) is(name string) bool { return n.space == nsW && n.name == name }

func (n *node) child(name string) *node {
	for _, k := range n.kids {
		if k.is(name) {
			return k
		}
	}
	return nil
}

// parseXML builds the element tree of doc with byte offsets.
func parseXML(doc []byte) (*node, error) {
	dec := xml.NewDecoder(bytes.NewReader(doc))
	var stack []*node
	var root *node
	for {
		pos := int(dec.InputOffset())
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnsupported, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{space: t.Name.Space, name: t.Name.Local, start: pos, openEnd: int(dec.InputOffset())}
			if len(t.Attr) > 0 {
				n.attrs = make(map[string]string, len(t.Attr))
				for _, a := range t.Attr {
					n.attrs[a.Name.Local] = a.Value
				}
			}
			if len(stack) > 0 {
				n.parent = stack[len(stack)-1]
				n.parent.kids = append(n.parent.kids, n)
			} else {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			n := stack[len(stack)-1]
			n.end = int(dec.InputOffset())
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				if top.is("t") || top.is("delText") {
					top.text += string(t)
				}
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("%w: empty document", ErrUnsupported)
	}
	return root, nil
}

// view says how revisions by the AI author are read.
type view struct {
	author string
	// rejectAI reads the document as if the AI's own changes were rejected
	// (its insertions vanish, its deletions come back). Revisions by anyone
	// else are always read as accepted.
	rejectAI bool
}

func (v view) isAI(n *node) bool { return v.author != "" && n.attrs["author"] == v.author }

// segment is a stretch of a paragraph's text and where it came from.
type segment struct {
	text     []rune
	a, b     int   // rune range within the paragraph text
	run      *node // the w:r, for editable segments
	wrapper  *node // the direct w:ins that holds run (nil if the run is a direct child of w:p)
	editable bool  // a plain-text run that can be split and re-emitted
}

type paragraph struct {
	n    *node
	segs []segment
	text []rune
}

func (p *paragraph) String() string { return string(p.text) }

// runText returns the current text of a run and whether it is a plain-text run.
func runText(r *node) (text string, plain bool) {
	plain = true
	hasText := false
	var b strings.Builder
	for _, k := range r.kids {
		switch {
		case k.is("rPr"), k.is("lastRenderedPageBreak"):
		case k.is("t"):
			b.WriteString(k.text)
			hasText = true
		case k.is("tab"):
			b.WriteByte('\t')
			plain = false
		case k.is("br"), k.is("cr"):
			b.WriteByte('\n')
			plain = false
		case k.is("noBreakHyphen"):
			b.WriteByte('-')
			plain = false
		case k.is("delText"), k.is("instrText"):
			plain = false
		default:
			plain = false
		}
	}
	return b.String(), plain && hasText
}

// opaqueText collects the text under n as it currently reads (deleted text
// excluded), without making any of it editable.
func opaqueText(n *node, v view) string {
	var b strings.Builder
	var walk func(*node)
	walk = func(n *node) {
		switch {
		case n.is("del"):
			if !(v.rejectAI && v.isAI(n)) {
				return
			}
			// rejected AI deletion: its text is back
			for _, d := range descendants(n, "delText") {
				b.WriteString(d.text)
			}
			return
		case n.is("ins"):
			if v.rejectAI && v.isAI(n) {
				return
			}
		case n.is("t"):
			b.WriteString(n.text)
			return
		case n.is("tab"):
			b.WriteByte('\t')
		case n.is("br"), n.is("cr"):
			b.WriteByte('\n')
		}
		for _, k := range n.kids {
			walk(k)
		}
	}
	walk(n)
	return b.String()
}

func descendants(n *node, name string) []*node {
	var out []*node
	var walk func(*node)
	walk = func(n *node) {
		for _, k := range n.kids {
			if k.is(name) {
				out = append(out, k)
			}
			walk(k)
		}
	}
	walk(n)
	return out
}

// readParagraph models the current text of w:p (direct runs and revisions only;
// nested paragraphs, e.g. in text boxes, are separate paragraphs).
func readParagraph(p *node, v view) *paragraph {
	para := &paragraph{n: p}
	add := func(s segment) {
		s.a = len(para.text)
		para.text = append(para.text, s.text...)
		s.b = len(para.text)
		if s.b > s.a || s.editable {
			para.segs = append(para.segs, s)
		}
	}
	for _, k := range p.kids {
		switch {
		case k.is("r"):
			t, plain := runText(k)
			add(segment{text: []rune(t), run: k, editable: plain})
		case k.is("ins"):
			if v.rejectAI && v.isAI(k) {
				continue
			}
			allRuns := true
			for _, kk := range k.kids {
				if !kk.is("r") {
					allRuns = false
				}
			}
			if !allRuns {
				add(segment{text: []rune(opaqueText(k, v))})
				continue
			}
			for _, r := range k.kids {
				t, plain := runText(r)
				add(segment{text: []rune(t), run: r, wrapper: k, editable: plain})
			}
		case k.is("del"):
			if t := opaqueText(k, v); t != "" {
				add(segment{text: []rune(t)})
			}
		case k.is("pPr"), k.is("bookmarkStart"), k.is("bookmarkEnd"), k.is("proofErr"),
			k.is("commentRangeStart"), k.is("commentRangeEnd"), k.is("permStart"), k.is("permEnd"):
		default:
			if t := opaqueText(k, v); t != "" {
				add(segment{text: []rune(t)})
			}
		}
	}
	return para
}

// readParagraphs lists the document's paragraphs in order, skipping the
// mc:Fallback copy of drawing content (it duplicates mc:Choice).
func readParagraphs(root *node, v view) []*paragraph {
	var out []*paragraph
	var walk func(*node)
	walk = func(n *node) {
		if n.space == nsMC && n.name == "Fallback" {
			return
		}
		if n.is("p") {
			out = append(out, readParagraph(n, v))
		}
		for _, k := range n.kids {
			walk(k)
		}
	}
	walk(root)
	return out
}

// numbered keeps the paragraphs that have text and numbers them from 1; this
// numbering is what the model sees and what edits refer to.
func numbered(paras []*paragraph) []*paragraph {
	var out []*paragraph
	for _, p := range paras {
		if strings.TrimSpace(string(p.text)) != "" {
			out = append(out, p)
		}
	}
	return out
}

// Paragraph is one numbered paragraph of the document's current text.
type Paragraph struct {
	Index int // 1-based
	Text  string
}

// Paragraphs returns the numbered, non-empty paragraphs of a .docx as they
// currently read (existing tracked insertions accepted, deletions dropped).
func Paragraphs(docx []byte) ([]Paragraph, error) {
	files, err := readZip(docx)
	if err != nil {
		return nil, err
	}
	root, err := parseXML(files.document)
	if err != nil {
		return nil, err
	}
	var out []Paragraph
	for i, p := range numbered(readParagraphs(root, view{})) {
		out = append(out, Paragraph{Index: i + 1, Text: string(p.text)})
	}
	return out, nil
}
