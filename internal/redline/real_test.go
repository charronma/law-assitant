package redline

import (
	"os"
	"strings"
	"testing"
)

// TestRealDocument redlines a real contract when REDLINE_REAL_DOCX points at one
// (it is skipped otherwise); REDLINE_REAL_OUT receives the result for inspection.
func TestRealDocument(t *testing.T) {
	path := os.Getenv("REDLINE_REAL_DOCX")
	if path == "" {
		t.Skip("REDLINE_REAL_DOCX not set")
	}
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	paras, err := Paragraphs(src)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d numbered paragraphs", len(paras))

	var plan Plan
	for _, p := range paras {
		for _, kw := range []string{"违约金", "支付", "解除", "管辖", "保密"} {
			rs := []rune(p.Text)
			i := strings.Index(p.Text, kw)
			if i < 0 {
				continue
			}
			start := len([]rune(p.Text[:i]))
			end := min(start+len([]rune(kw))+6, len(rs))
			find := string(rs[start:end])
			plan.Edits = append(plan.Edits, Edit{Para: p.Index, Find: find, Replace: str(find + "【AI修订】"), Comment: "测试批注：" + kw})
			break
		}
	}
	plan.Insertions = append(plan.Insertions, Insertion{After: len(paras) / 2, Text: "新增条款：乙方应当为派遣员工依法缴纳社会保险。", Comment: "补充缺失条款"})
	t.Logf("plan: %d edits, %d insertions", len(plan.Edits), len(plan.Insertions))

	res := mustApply(t, src, plan)
	t.Logf("applied %d, skipped %d", res.AppliedCount(), len(res.Skipped))
	for _, s := range res.Skipped {
		t.Logf("  skipped %s #%d (para %d): %s", s.Kind, s.Index, s.Para, s.Reason)
		if strings.Contains(s.Reason, "内部校验") {
			t.Errorf("internal verification failure on a real document")
		}
	}
	// The original text is recoverable by rejecting the AI's changes.
	orig := texts(t, src, false)
	back := texts(t, res.Docx, true)
	eq(t, "rejected view equals the original", back, orig)
	checkNewIDs(t, src, res.Docx)
	checkComments(t, res.Docx)
	if out := os.Getenv("REDLINE_REAL_OUT"); out != "" {
		if err := os.WriteFile(out, res.Docx, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
