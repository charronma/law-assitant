package handler

import (
	"archive/zip"
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func (e *testEnv) export(bearer, body string) (code int, header http.Header, data []byte) {
	rec := e.do("POST", "/api/export/docx", bearer, strings.NewReader(body), "application/json")
	return rec.Code, rec.Header(), rec.Body.Bytes()
}

func TestExportDocx(t *testing.T) {
	e := newTestEnv(t)
	code, h, data := e.export(token(t, "alice"), `{"content":"# 民事起诉状\n\n原告：张三\n\n| 项 | 值 |\n|---|---|\n| 诉讼请求 | 返还借款 |"}`)
	if code != http.StatusOK {
		t.Fatalf("got %d %s", code, data)
	}
	if ct := h.Get("Content-Type"); !strings.Contains(ct, "wordprocessingml.document") {
		t.Errorf("content-type %q", ct)
	}
	cd := h.Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, "filename*=UTF-8''%E6%B0%91%E4%BA%8B%E8%B5%B7%E8%AF%89%E7%8A%B6.docx") {
		t.Errorf("content-disposition %q (filename should come from the first heading)", cd)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a docx: %v", err)
	}
	var doc string
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, _ := f.Open()
			var b bytes.Buffer
			_, _ = b.ReadFrom(rc)
			doc = b.String()
		}
	}
	// The heading must not be duplicated as a separate Title paragraph.
	if strings.Count(doc, "民事起诉状") != 1 || !strings.Contains(doc, "返还借款") {
		t.Errorf("unexpected document body: %s", doc)
	}
}

func TestExportDocxValidation(t *testing.T) {
	e := newTestEnv(t)
	if code, _, _ := e.export("", `{"content":"x"}`); code != http.StatusUnauthorized {
		t.Errorf("anonymous export: %d", code)
	}
	alice := token(t, "alice")
	for name, body := range map[string]string{
		"empty":    `{"content":"  "}`,
		"bad json": `{`,
	} {
		if code, _, _ := e.export(alice, body); code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, code)
		}
	}
	big := `{"content":"` + strings.Repeat("a", maxExportBodyBytes) + `"}`
	if code, _, _ := e.export(alice, big); code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized: %d", code)
	}
}

func TestContentDispositionCannotBeInjected(t *testing.T) {
	got := contentDisposition("a\r\nSet-Cookie: x=1\";/b.docx")
	if strings.ContainsAny(got, "\r\n") || strings.Count(got, `"`) != 2 {
		t.Errorf("unsafe header: %q", got)
	}
}
