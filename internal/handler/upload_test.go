package handler

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type uploadResp struct {
	FileID    string `json:"file_id"`
	Chars     int    `json:"chars"`
	Preview   string `json:"preview"`
	Truncated bool   `json:"truncated"`
}

func (e *testEnv) uploadRaw(t *testing.T, bearer, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(content)
	_ = mw.Close()
	return e.do("POST", "/api/upload", bearer, &buf, mw.FormDataContentType())
}

func (e *testEnv) upload(t *testing.T, bearer, filename string, content []byte) uploadResp {
	t.Helper()
	rec := e.uploadRaw(t, bearer, filename, content)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload %s: %d %s", filename, rec.Code, rec.Body)
	}
	var up uploadResp
	if err := json.Unmarshal(rec.Body.Bytes(), &up); err != nil || up.FileID == "" {
		t.Fatalf("bad upload response: %v %s", err, rec.Body)
	}
	return up
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../tool/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func storedFiles(t *testing.T, dir string) int {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return len(ents)
}

func TestUploadRealDocumentsReportExtractedText(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	for _, name := range []string{"contract.docx", "contract.pdf"} {
		up := e.upload(t, alice, name, fixture(t, name))
		if up.Chars < 100 {
			t.Errorf("%s: chars=%d, extraction looks empty", name, up.Chars)
		}
		if !strings.Contains(up.Preview, "劳动合同") {
			t.Errorf("%s: preview %q lacks the title", name, up.Preview)
		}
		if up.Truncated {
			t.Errorf("%s: unexpectedly truncated", name)
		}
	}
}

func TestUploadRejectsUnreadableDocuments(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		content  []byte
		status   int
		code     string
		hint     string
	}{
		{"scanned pdf", "scan.pdf", fixture(t, "scanned.pdf"), 422, "NO_TEXT", "OCR"},
		{"legacy doc", "old.doc", []byte("\xd0\xcf\x11\xe0whatever"), 422, "UNSUPPORTED_FORMAT", ".docx"},
		{"corrupt docx", "bad.docx", []byte("this is not a zip"), 422, "EXTRACT_FAILED", "损坏"},
		{"pdf extension on non-pdf", "fake.pdf", []byte("hello"), 422, "EXTRACT_FAILED", "损坏"},
		{"empty txt", "empty.txt", []byte("  \n "), 422, "NO_TEXT", "OCR"},
		{"unknown extension", "a.exe", []byte("MZ"), 422, "UNSUPPORTED_FORMAT", ".docx"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			rec := e.uploadRaw(t, token(t, "alice"), tc.filename, tc.content)
			if rec.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.status, rec.Body)
			}
			var body struct{ Code, Message, Error string }
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != tc.code {
				t.Errorf("code %q, want %q", body.Code, tc.code)
			}
			if !strings.Contains(body.Message, tc.hint) || body.Error != body.Message {
				t.Errorf("message %q / error %q should mention %q", body.Message, body.Error, tc.hint)
			}
			if n := storedFiles(t, e.dir); n != 0 {
				t.Errorf("rejected upload left %d file(s) on disk", n)
			}
		})
	}
}

func TestDocumentsAreInjectedAsUserDataForEveryModule(t *testing.T) {
	for _, module := range []string{"contract", "evidence_org", "consult"} {
		t.Run(module, func(t *testing.T) {
			e := newTestEnv(t)
			alice := token(t, "alice")
			up := e.upload(t, alice, "contract.docx", fixture(t, "contract.docx"))
			sid := e.createSessionFor(t, alice, module)
			rec := e.chat(t, alice, map[string]any{"session_id": sid, "message": "请审查", "file_ids": []string{up.FileID}})
			if rec.Code != http.StatusOK {
				t.Fatalf("got %d: %s", rec.Code, rec.Body)
			}
			e.llm.mu.Lock()
			defer e.llm.mu.Unlock()
			in := e.llm.seen[0]
			var docIdx = -1
			for i, m := range in {
				if strings.Contains(m.Content, "违约金人民币十万元") {
					if m.Role != "user" {
						t.Errorf("document injected with role %q, want user", m.Role)
					}
					docIdx = i
				}
			}
			if docIdx < 0 {
				t.Fatalf("document text never reached the model: %+v", in)
			}
			if !strings.Contains(in[docIdx].Content, "<document") {
				t.Errorf("document is not delimited: %q", in[docIdx].Content)
			}
			if last := in[len(in)-1]; last.Content != "请审查" {
				t.Errorf("the user's question must stay last, got %q", last.Content)
			}
		})
	}
}

func TestChatWithUnreadableFilesFailsBeforeCallingTheModel(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	sid := e.createSessionFor(t, alice, "contract")
	rec := e.chat(t, alice, map[string]any{"session_id": sid, "message": "审查", "file_ids": []string{"gone"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	if b := decodeErr(t, rec); b.Code != "FILE_UNAVAILABLE" {
		t.Errorf("code %q", b.Code)
	}
	if got := e.llm.models(); len(got) != 0 {
		t.Errorf("model must not be called, got %v", got)
	}
	if h := e.history(t, "alice", sid); len(h) != 0 {
		t.Errorf("nothing may be persisted: %+v", h)
	}
}
