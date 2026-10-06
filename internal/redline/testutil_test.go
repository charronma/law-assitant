package redline

import (
	"archive/zip"
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	testAuthor = "AI 法律助手"
	rootOpen   = `<w:document xmlns:w="` + nsW + `" xmlns:mc="` + nsMC + `" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`
)

var fixedNow = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

// buildDocx makes a minimal package around body (the content of w:body).
func buildDocx(t testing.TB, body string, extra map[string]string) []byte {
	t.Helper()
	parts := map[string]string{
		"[Content_Types].xml":          `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":                  `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"word/document.xml":            `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + rootOpen + `<w:body>` + body + `</w:body></w:document>`,
		"word/styles.xml":              `<?xml version="1.0" encoding="UTF-8"?><w:styles xmlns:w="` + nsW + `"/>`,
	}
	for k, v := range extra {
		parts[k] = v
	}
	order := []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml", "word/_rels/document.xml.rels", "word/styles.xml"}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	seen := map[string]bool{}
	for _, name := range order {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(parts[name]))
		seen[name] = true
	}
	for name, v := range parts {
		if !seen[name] {
			w, _ := zw.Create(name)
			_, _ = w.Write([]byte(v))
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func run(text string) string { return `<w:r><w:t xml:space="preserve">` + text + `</w:t></w:r>` }
func boldRun(text string) string {
	return `<w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">` + text + `</w:t></w:r>`
}
func para(inner string) string { return `<w:p>` + inner + `</w:p>` }

func str(s string) *string { return &s }

func mustApply(t testing.TB, docx []byte, plan Plan) Result {
	t.Helper()
	res, err := Apply(docx, plan, testAuthor, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func documentXML(t testing.TB, docx []byte) string {
	t.Helper()
	f, err := readZip(docx)
	if err != nil {
		t.Fatal(err)
	}
	return string(f.document)
}

// texts reads the numbered paragraphs of a package; rejectAI reads it as if the AI's changes were rejected.
func texts(t testing.TB, docx []byte, rejectAI bool) []string {
	t.Helper()
	f, err := readZip(docx)
	if err != nil {
		t.Fatal(err)
	}
	root, err := parseXML(f.document)
	if err != nil {
		t.Fatalf("result is not well-formed: %v", err)
	}
	var out []string
	for _, p := range numbered(readParagraphs(root, view{author: testAuthor, rejectAI: rejectAI})) {
		out = append(out, string(p.text))
	}
	return out
}

func eq(t testing.TB, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("%s:\n got  %q\n want %q", what, got, want)
	}
}

var idRe = regexp.MustCompile(`w:id="(\d+)"`)

// checkIDs fails if any w:id in document.xml (tracked changes, comment anchors excluded) repeats.
func checkIDs(t testing.TB, docx []byte) {
	t.Helper()
	doc := documentXML(t, docx)
	seen := map[string]string{}
	for _, tag := range regexp.MustCompile(`<w:(ins|del|bookmarkStart)\b[^>]*>`).FindAllString(doc, -1) {
		m := idRe.FindStringSubmatch(tag)
		if m == nil {
			continue
		}
		if prev, dup := seen[m[1]]; dup {
			t.Errorf("duplicate w:id %s: %s and %s", m[1], prev, tag)
		}
		seen[m[1]] = tag
	}
}

// checkComments verifies every comment anchor has a start, an end, a reference and a body.
func checkComments(t testing.TB, docx []byte) {
	t.Helper()
	f, err := readZip(docx)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(f.document)
	starts := regexp.MustCompile(`<w:commentRangeStart w:id="(\d+)"/>`).FindAllStringSubmatch(doc, -1)
	for _, m := range starts {
		id := m[1]
		for _, want := range []string{
			fmt.Sprintf(`<w:commentRangeEnd w:id="%s"/>`, id),
			fmt.Sprintf(`<w:commentReference w:id="%s"/>`, id),
		} {
			if strings.Count(doc, want) != 1 {
				t.Errorf("comment %s: want exactly one %s", id, want)
			}
		}
		if f.comments == nil || !strings.Contains(string(f.comments), fmt.Sprintf(`<w:comment w:id="%s"`, id)) {
			t.Errorf("comment %s has no body in comments.xml", id)
		}
		if strings.Index(doc, fmt.Sprintf(`<w:commentRangeStart w:id="%s"/>`, id)) > strings.Index(doc, fmt.Sprintf(`<w:commentRangeEnd w:id="%s"/>`, id)) {
			t.Errorf("comment %s ends before it starts", id)
		}
	}
}

func regexpMust(s string) *regexp.Regexp { return regexp.MustCompile(s) }

// checkNewIDs verifies that every id the AI added is unique and unused by the original
// (a real document may legitimately repeat ids across annotation kinds).
func checkNewIDs(t testing.TB, orig, out []byte) {
	t.Helper()
	used := map[string]bool{}
	for _, m := range idRe.FindAllStringSubmatch(documentXML(t, orig), -1) {
		used[m[1]] = true
	}
	seen := map[string]bool{}
	for _, tag := range regexp.MustCompile(`<w:(?:ins|del)\b[^>]*w:author="`+testAuthor+`"[^>]*>`).FindAllString(documentXML(t, out), -1) {
		id := idRe.FindStringSubmatch(tag)[1]
		if used[id] || seen[id] {
			t.Errorf("AI revision id %s collides: %s", id, tag)
		}
		seen[id] = true
	}
}
