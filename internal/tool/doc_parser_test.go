package tool

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func write(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// docx builds a minimal .docx whose word/document.xml has the given body.
func docx(t *testing.T, body string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("word/document.xml")
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"><w:body>` + body + `</w:body></w:document>`))
	_ = zw.Close()
	return write(t, "t.docx", buf.Bytes())
}

func parse(t *testing.T, path string) (Result, error) {
	t.Helper()
	return NewDocumentParser().Parse(path)
}

const wantContract = "乙方违约的，应当向甲方支付违约金人民币十万元；甲方不承担任何责任。"

func TestRealDocx(t *testing.T) {
	res, err := parse(t, "testdata/contract.docx")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"劳动合同", "甲方（用人单位）：北京某某科技有限公司", "第一条 合同期限", "试用期为三个月", "基本工资 | 15000", "岗位津贴 | 3000", wantContract} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("missing %q in:\n%s", want, res.Text)
		}
	}
	if !strings.Contains(res.Text, "\n") {
		t.Error("paragraph boundaries lost")
	}
}

func TestRealPDF(t *testing.T) {
	res, err := parse(t, "testdata/contract.pdf")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"劳动合同", "北京某某科技有限公司", "试用期为三个月", "15000", "违约金人民币十万元"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("missing %q in:\n%s", want, res.Text)
		}
	}
}

func TestScannedPDFIsAnExplicitFailure(t *testing.T) {
	if _, err := parse(t, "testdata/scanned.pdf"); !errors.Is(err, ErrNoText) {
		t.Fatalf("got %v, want ErrNoText", err)
	}
}

func TestDocxStructure(t *testing.T) {
	cases := []struct {
		name, body, want, notWant string
	}{
		{"paragraphs", `<w:p><w:r><w:t>甲</w:t></w:r></w:p><w:p><w:r><w:t>乙</w:t></w:r></w:p>`, "甲\n乙", ""},
		{"runs join within a paragraph", `<w:p><w:r><w:t>违约</w:t></w:r><w:r><w:t>金</w:t></w:r></w:p>`, "违约金", ""},
		{"tabs and breaks", `<w:p><w:r><w:t>a</w:t><w:tab/><w:t>b</w:t><w:br/><w:t>c</w:t></w:r></w:p>`, "a\tb\nc", ""},
		{"table cells", `<w:tbl><w:tr><w:tc><w:p><w:r><w:t>项目</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>金额</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`, "项目 | 金额", ""},
		{"tracked deletions are not text", `<w:p><w:del><w:r><w:delText>旧条款</w:delText></w:r></w:del><w:ins><w:r><w:t>新条款</w:t></w:r></w:ins></w:p>`, "新条款", "旧条款"},
		{"alternate content fallback is not duplicated", `<w:p><mc:AlternateContent><mc:Choice><w:r><w:t>文本框</w:t></w:r></mc:Choice><mc:Fallback><w:r><w:t>文本框</w:t></w:r></mc:Fallback></mc:AlternateContent></w:p>`, "文本框", "文本框文本框"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := parse(t, docx(t, tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(res.Text, tc.want) {
				t.Errorf("got %q, want it to contain %q", res.Text, tc.want)
			}
			if tc.notWant != "" && strings.Contains(res.Text, tc.notWant) {
				t.Errorf("got %q, must not contain %q", res.Text, tc.notWant)
			}
		})
	}
}

func TestDocxFailures(t *testing.T) {
	t.Run("not a zip", func(t *testing.T) {
		if _, err := parse(t, write(t, "x.docx", []byte("plain text"))); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("zip without document.xml", func(t *testing.T) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		_, _ = zw.Create("other.xml")
		_ = zw.Close()
		if _, err := parse(t, write(t, "x.docx", buf.Bytes())); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("empty body", func(t *testing.T) {
		if _, err := parse(t, docx(t, `<w:p/>`)); !errors.Is(err, ErrNoText) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("malformed xml", func(t *testing.T) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create("word/document.xml")
		_, _ = w.Write([]byte(`<w:document><w:t>abc</w:p></w:document>`))
		_ = zw.Close()
		if _, err := parse(t, write(t, "x.docx", buf.Bytes())); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("decompression bomb", func(t *testing.T) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create("word/document.xml")
		_, _ = w.Write([]byte(`<w:document xmlns:w="x"><w:body><w:p><w:r><w:t>`))
		chunk := bytes.Repeat([]byte("a"), 1<<20)
		for i := 0; i < maxDocxXMLBytes>>20+2; i++ {
			_, _ = w.Write(chunk)
		}
		_ = zw.Close()
		if _, err := parse(t, write(t, "x.docx", buf.Bytes())); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("got %v, want ErrTooLarge", err)
		}
	})
}

func TestPDFFailures(t *testing.T) {
	for name, data := range map[string][]byte{
		"not a pdf":     []byte("hello world"),
		"truncated pdf": []byte("%PDF-1.7\n1 0 obj\n<<"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parse(t, write(t, "x.pdf", data)); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("got %v, want ErrCorrupt", err)
			}
		})
	}
}

func TestTextEncodings(t *testing.T) {
	const s = "合同第一条：保密义务"
	gbk, err := simplifiedchinese.GB18030.NewEncoder().String(s)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"utf8":     []byte(s),
		"utf8 bom": append([]byte{0xEF, 0xBB, 0xBF}, s...),
		"gbk":      []byte(gbk),
		"crlf":     []byte(strings.ReplaceAll(s, "：", "：\r\n")),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			res, err := parse(t, write(t, "a.txt", data))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(res.Text, "保密义务") || strings.ContainsAny(res.Text, "\r\uFEFF\uFFFD") {
				t.Errorf("got %q", res.Text)
			}
		})
	}
	if _, err := parse(t, write(t, "a.md", []byte("a\x00b"))); !errors.Is(err, ErrCorrupt) {
		t.Errorf("binary content in .md: got %v", err)
	}
}

func TestTruncationIsReported(t *testing.T) {
	res, err := parse(t, write(t, "big.txt", []byte(strings.Repeat("法", MaxExtractedRunes+10))))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || len([]rune(res.Text)) != MaxExtractedRunes {
		t.Errorf("truncated=%v runes=%d", res.Truncated, len([]rune(res.Text)))
	}
	small, _ := parse(t, write(t, "s.txt", []byte("短")))
	if small.Truncated {
		t.Error("short text flagged as truncated")
	}
}

func TestFormats(t *testing.T) {
	if _, err := parse(t, write(t, "a.doc", []byte("x"))); !errors.Is(err, ErrLegacyDoc) {
		t.Errorf(".doc: got %v", err)
	}
	if _, err := parse(t, write(t, "a.xlsx", []byte("x"))); !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf(".xlsx: got %v", err)
	}
	for f, want := range map[string]bool{"a.PDF": true, "a.docx": true, "a.txt": true, "a.md": true, "a.doc": false, "a.png": false} {
		if IsSupportedFormat(f) != want {
			t.Errorf("IsSupportedFormat(%q) != %v", f, want)
		}
	}
}
