package export

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"law-assistant/internal/tool"
)

const sample = "# 劳动合同审查报告\n\n## 一、主要风险\n\n- **违约金过高**：约定十万元，远超实际损失\n- 试用期*超过*法定上限\n  - 子项 A\n\n1. 修改第三条\n2. 补充社保条款\n\n| 条款 | 风险等级 |\n|------|----------|\n| 第三条 | **高** |\n| 第五条 | 中 |\n\n> 注意：以《劳动合同法》为准\n\n```\nSELECT 1 < 2 && 3 > 2\n```\n\n---\n\n普通段落第一行\n第二行 with `code` and [链接](https://example.com)。\n"

func build(t *testing.T, title, md string) (string, []byte) {
	t.Helper()
	data, err := MarkdownToDocx(title, md)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	var doc string
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		// Every part must be well-formed XML.
		d := xml.NewDecoder(bytes.NewReader(b))
		for {
			if _, err := d.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s is not well-formed: %v", f.Name, err)
			}
		}
		if f.Name == "word/document.xml" {
			doc = string(b)
		}
	}
	if doc == "" {
		t.Fatal("no word/document.xml")
	}
	return doc, data
}

func TestStructure(t *testing.T) {
	doc, _ := build(t, "", sample)
	for _, want := range []string{
		`w:pStyle w:val="Heading1"`, `w:pStyle w:val="Heading2"`, `<w:tbl>`, `<w:b/>`, `<w:i/>`,
		`SELECT 1 &lt; 2 &amp;&amp; 3 &gt; 2`, // escaped, not interpreted
		`w:left="840"`,                        // nested bullet indented deeper
		"链接 (https://example.com)",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(doc, "**") || strings.Contains(doc, "```") || strings.Contains(doc, "|---") {
		t.Error("raw Markdown syntax leaked into the document")
	}
	if !strings.Contains(doc, Disclaimer) {
		t.Error("disclaimer missing")
	}
}

func TestRoundTripThroughOurParser(t *testing.T) {
	_, data := build(t, "审查报告", sample)
	p := filepath.Join(t.TempDir(), "out.docx")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := tool.NewDocumentParser().Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"审查报告", "劳动合同审查报告", "违约金过高：约定十万元", "1. 修改第三条", "条款 | 风险等级", "第三条 | 高", "普通段落第一行\n第二行 with code and 链接 (https://example.com)。", Disclaimer} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("missing %q in:\n%s", want, res.Text)
		}
	}
}

func TestHostileInput(t *testing.T) {
	md := "<script>alert(1)</script> & \x00\x01\x1b ok ￾\n\n| a |\n|---|\n| " + strings.Repeat("x|", 50) + "\n\n```\nunterminated"
	build(t, "<b>&", md) // must stay well-formed XML
	build(t, "", "")
	build(t, "", "|\n|")
}

func TestFirstHeading(t *testing.T) {
	if got := FirstHeading("intro\n\n## **起诉状** ##\n# later"); got != "起诉状" {
		t.Errorf("got %q", got)
	}
	if FirstHeading("no heading") != "" {
		t.Error("expected empty")
	}
}
