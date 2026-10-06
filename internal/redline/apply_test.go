package redline

import (
	"strings"
	"testing"
)

func TestReplaceInsideOneRunKeepsFormattingAndComments(t *testing.T) {
	src := buildDocx(t, para(boldRun("违约金为合同总额的30%，逾期支付。")), nil)
	res := mustApply(t, src, Plan{Edits: []Edit{{Para: 1, Find: "违约金为合同总额的30%", Replace: str("违约金为合同总额的10%"), Comment: "违约金过高，建议下调"}}})
	if len(res.Skipped) != 0 || res.AppliedCount() != 1 {
		t.Fatalf("applied=%v skipped=%+v", res.Applied, res.Skipped)
	}
	eq(t, "accepted", texts(t, res.Docx, false), []string{"违约金为合同总额的10%，逾期支付。"})
	eq(t, "rejected", texts(t, res.Docx, true), []string{"违约金为合同总额的30%，逾期支付。"})

	doc := documentXML(t, res.Docx)
	// Only the changed character is marked, not the whole phrase.
	if !strings.Contains(doc, `<w:delText xml:space="preserve">3</w:delText>`) {
		t.Errorf("expected a minimal deletion of '3':\n%s", doc)
	}
	// Every piece keeps the run's bold formatting, including the inserted text.
	if strings.Count(doc, `<w:rPr><w:b/></w:rPr>`) < 4 {
		t.Errorf("formatting lost:\n%s", doc)
	}
	checkIDs(t, res.Docx)
	checkComments(t, res.Docx)

	f, _ := readZip(res.Docx)
	if !strings.Contains(string(f.comments), "违约金过高，建议下调") || !strings.Contains(string(f.comments), `w:author="AI 法律助手"`) {
		t.Errorf("comments.xml: %s", f.comments)
	}
	if !strings.Contains(string(f.rels), relTypeComments) || !strings.Contains(string(f.contentType), "/word/comments.xml") {
		t.Errorf("comments part not registered:\nrels=%s\nct=%s", f.rels, f.contentType)
	}
}

func TestUntouchedPartsAreByteIdentical(t *testing.T) {
	before := para(boldRun("第一段保持不变。")) + `<w:p w:rsidR="00AB12"><w:pPr><w:jc w:val="center"/></w:pPr>` + run("第二段付款期限30日") + `</w:p>`
	after := para(run("第三段也不变")) + `<w:sectPr><w:pgSz w:w="11906"/></w:sectPr>`
	src := buildDocx(t, before+`<w:p><w:r><w:t>要改的这一段话</w:t></w:r></w:p>`+after, nil)
	res := mustApply(t, src, Plan{Edits: []Edit{{Para: 3, Find: "这一段话", Replace: str("那一段话")}}})
	doc, orig := documentXML(t, res.Docx), documentXML(t, src)
	i := strings.Index(orig, `<w:p><w:r><w:t>要改的这一段话`)
	if !strings.HasPrefix(doc, orig[:i]) {
		t.Error("content before the edited paragraph changed")
	}
	if !strings.HasSuffix(doc, after+`</w:body></w:document>`) {
		t.Error("content after the edited paragraph changed")
	}
}

func TestEditSpanningSeveralRunsWithDifferentFormatting(t *testing.T) {
	src := buildDocx(t, para(run("甲方应当在")+boldRun("收到发票后")+run("30")+boldRun("日内付款")+run("。")), nil)
	res := mustApply(t, src, Plan{Edits: []Edit{{Para: 1, Find: "收到发票后30日内付款", Replace: str("收到发票后15日内付清全部款项"), Comment: "缩短账期"}}})
	if len(res.Skipped) != 0 {
		t.Fatalf("%+v", res.Skipped)
	}
	eq(t, "accepted", texts(t, res.Docx, false), []string{"甲方应当在收到发票后15日内付清全部款项。"})
	eq(t, "rejected", texts(t, res.Docx, true), []string{"甲方应当在收到发票后30日内付款。"})
	checkIDs(t, res.Docx)
	checkComments(t, res.Docx)
}

func TestEditsInsideAndAroundExistingRevisions(t *testing.T) {
	body := para(run("前文，") +
		`<w:ins w:id="5" w:author="张三" w:date="2026-01-01T00:00:00Z"><w:r><w:t>新增的条款内容</w:t></w:r></w:ins>` +
		`<w:del w:id="6" w:author="张三" w:date="2026-01-01T00:00:00Z"><w:r><w:delText>已被删除的内容</w:delText></w:r></w:del>` +
		run("，结尾。"))
	src := buildDocx(t, body, nil)
	eq(t, "source reads as accepted", texts(t, src, false), []string{"前文，新增的条款内容，结尾。"})

	res := mustApply(t, src, Plan{Edits: []Edit{
		{Para: 1, Find: "条款内容", Replace: str("条款文字"), Comment: "措辞"},
		{Para: 1, Find: "结尾", Replace: str("终止")},
	}})
	if len(res.Skipped) != 0 {
		t.Fatalf("%+v", res.Skipped)
	}
	eq(t, "accepted", texts(t, res.Docx, false), []string{"前文，新增的条款文字，终止。"})
	eq(t, "rejected (only the AI's changes)", texts(t, res.Docx, true), []string{"前文，新增的条款内容，结尾。"})

	doc := documentXML(t, res.Docx)
	if !strings.Contains(doc, `w:author="张三"`) || !strings.Contains(doc, "已被删除的内容") {
		t.Error("the existing revisions must survive")
	}
	// Deleting someone else's insertion nests our deletion inside their w:ins.
	if !regexpMust(`<w:ins [^>]*w:author="张三"[^>]*><w:del [^>]*w:author="AI 法律助手"`).MatchString(doc) {
		t.Errorf("expected w:ins > w:del nesting:\n%s", doc)
	}
	checkIDs(t, res.Docx)
}

func TestRunsWeCannotEditAreSkippedNotCorrupted(t *testing.T) {
	body := para(run("请见")+`<w:hyperlink r:id="rId9"><w:r><w:t>附件一</w:t></w:r></w:hyperlink>`+run("所列清单。")) +
		para(run("日期：")+`<w:r><w:tab/><w:t>2026年</w:t></w:r>`+run("签署"))
	src := buildDocx(t, body, nil)
	res := mustApply(t, src, Plan{Edits: []Edit{
		{Para: 1, Find: "附件一所列", Replace: str("附件二所列")}, // crosses a hyperlink
		{Para: 1, Find: "所列清单", Replace: str("所述清单")},   // plain runs after the link: fine
		{Para: 2, Find: "2026年", Replace: str("2027年")}, // run with a tab: not editable
		{Para: 2, Find: "签署", Replace: str("盖章")},       // offsets must still line up after the tab
	}})
	if len(res.Skipped) != 2 || res.AppliedCount() != 2 {
		t.Fatalf("applied=%v skipped=%+v", res.Applied, res.Skipped)
	}
	for _, s := range res.Skipped {
		if !strings.Contains(s.Reason, "无法编辑") {
			t.Errorf("reason %q", s.Reason)
		}
	}
	eq(t, "accepted", texts(t, res.Docx, false), []string{"请见附件一所述清单。", "日期：\t2026年盖章"})
	eq(t, "rejected", texts(t, res.Docx, true), []string{"请见附件一所列清单。", "日期：\t2026年签署"})
}

func TestLocatingText(t *testing.T) {
	src := buildDocx(t, para(run("甲  方：北京公司"))+para(run("乙方向乙方支付"))+para(run("原文")), nil)
	res := mustApply(t, src, Plan{Edits: []Edit{
		{Para: 1, Find: "甲方：北京公司", Replace: str("甲方：上海公司")}, // model dropped the double space
		{Para: 2, Find: "乙方", Replace: str("丙方")},           // appears twice
		{Para: 3, Find: "不存在的原文", Replace: str("x")},
		{Para: 9, Find: "原文", Replace: str("x")},
		{Para: 3, Find: "", Replace: str("x")},
		{Para: 3, Find: "原文", Replace: str("原文")}, // nothing to do and no comment
	}})
	reasons := map[int]string{}
	for _, s := range res.Skipped {
		reasons[s.Index] = s.Reason
	}
	want := map[int]string{1: "多次", 2: "找到", 3: "不存在", 4: "为空", 5: "没有修改"}
	for idx, frag := range want {
		if !strings.Contains(reasons[idx], frag) {
			t.Errorf("edit %d: reason %q should mention %q", idx, reasons[idx], frag)
		}
	}
	if res.AppliedCount() != 1 || res.Applied[0].Index != 0 {
		t.Fatalf("applied=%v", res.Applied)
	}
	// The model normalised the double space, so its replacement text is what ends up in the document.
	eq(t, "accepted", texts(t, res.Docx, false), []string{"甲方：上海公司", "乙方向乙方支付", "原文"})
	eq(t, "rejected", texts(t, res.Docx, true), []string{"甲  方：北京公司", "乙方向乙方支付", "原文"})
}

func TestOverlappingEditsKeepTheFirst(t *testing.T) {
	src := buildDocx(t, para(run("甲方应当按时支付全部货款")), nil)
	res := mustApply(t, src, Plan{Edits: []Edit{
		{Para: 1, Find: "按时支付全部", Replace: str("按期支付")},
		{Para: 1, Find: "支付全部货款", Replace: str("付清货款")},
	}})
	if res.AppliedCount() != 1 || len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0].Reason, "重叠") {
		t.Fatalf("applied=%v skipped=%+v", res.Applied, res.Skipped)
	}
	eq(t, "accepted", texts(t, res.Docx, false), []string{"甲方应当按期支付货款"})
}

func TestPureInsertionAndDeletionAndCommentOnly(t *testing.T) {
	src := buildDocx(t, para(run("付款期限为30日"))+para(run("本合同自双方盖章之日起生效，不得单方解除。"))+para(run("争议由甲方所在地法院管辖。")), nil)
	res := mustApply(t, src, Plan{Edits: []Edit{
		{Para: 1, Find: "付款期限为30日", Replace: str("付款期限为30日，逾期按日万分之五支付违约金"), Comment: "补充逾期责任"},
		{Para: 2, Find: "，不得单方解除", Replace: str(""), Comment: "删除不合理限制"},
		{Para: 3, Find: "甲方所在地法院", Comment: "管辖约定偏向对方，需协商"},
	}})
	if len(res.Skipped) != 0 {
		t.Fatalf("%+v", res.Skipped)
	}
	eq(t, "accepted", texts(t, res.Docx, false), []string{
		"付款期限为30日，逾期按日万分之五支付违约金",
		"本合同自双方盖章之日起生效。",
		"争议由甲方所在地法院管辖。", // comment only: text unchanged
	})
	eq(t, "rejected", texts(t, res.Docx, true), []string{"付款期限为30日", "本合同自双方盖章之日起生效，不得单方解除。", "争议由甲方所在地法院管辖。"})
	checkComments(t, res.Docx)
	checkIDs(t, res.Docx)
	if n := strings.Count(documentXML(t, res.Docx), "<w:commentRangeStart"); n != 3 {
		t.Errorf("want 3 comment anchors, got %d", n)
	}
}

func TestInsertionAtTheStartOfAParagraph(t *testing.T) {
	src := buildDocx(t, para(boldRun("第一条 总则")), nil)
	res := mustApply(t, src, Plan{Edits: []Edit{{Para: 1, Find: "第一条 总则", Replace: str("（修订）第一条 总则"), Comment: "x"}}})
	if len(res.Skipped) != 0 {
		t.Fatalf("%+v", res.Skipped)
	}
	eq(t, "accepted", texts(t, res.Docx, false), []string{"（修订）第一条 总则"})
	eq(t, "rejected", texts(t, res.Docx, true), []string{"第一条 总则"})
	checkComments(t, res.Docx)
}

func TestInsertedParagraphContinuesNumbering(t *testing.T) {
	ppr := `<w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="3"/></w:numPr><w:jc w:val="both"/><w:rPr><w:b/></w:rPr></w:pPr>`
	body := `<w:p>` + ppr + boldRun("第一款内容") + `</w:p>` + para(run("无格式段落")) +
		`<w:p><w:pPr><w:jc w:val="left"/><w:sectPr><w:pgSz w:w="1"/></w:sectPr></w:pPr>` + run("带分节符") + `</w:p>`
	src := buildDocx(t, body, nil)
	res := mustApply(t, src, Plan{Insertions: []Insertion{
		{After: 1, Text: "新增：乙方应当对派遣员工的工伤承担责任。", Comment: "补充缺失条款"},
		{After: 2, Text: "新增第二处"},
		{After: 3, Text: "新增第三处"},
		{After: 7, Text: "越界"},
		{After: 1, Text: "   "},
	}})
	if res.AppliedCount() != 3 || len(res.Skipped) != 2 {
		t.Fatalf("applied=%v skipped=%+v", res.Applied, res.Skipped)
	}
	eq(t, "accepted", texts(t, res.Docx, false), []string{
		"第一款内容", "新增：乙方应当对派遣员工的工伤承担责任。", "无格式段落", "新增第二处", "带分节符", "新增第三处"})
	eq(t, "rejected", texts(t, res.Docx, true), []string{"第一款内容", "无格式段落", "带分节符"})

	doc := documentXML(t, res.Docx)
	// The new paragraph copies the numbering and marks its paragraph mark as inserted.
	if !regexpMust(`<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="3"/></w:numPr><w:jc w:val="both"/><w:rPr><w:ins [^>]*/><w:b/></w:rPr></w:pPr>`).MatchString(doc) {
		t.Errorf("numbering/rPr not carried over:\n%s", doc)
	}
	// The copy of a paragraph that ends a section must not copy the section break.
	if strings.Count(doc, "<w:sectPr>") != 1 {
		t.Errorf("sectPr duplicated:\n%s", doc)
	}
	checkIDs(t, res.Docx)
	checkComments(t, res.Docx)
}

func TestEditingTableCellsAndTextBoxes(t *testing.T) {
	tbl := `<w:tbl><w:tr><w:tc><w:p>` + run("项目") + `</w:p></w:tc><w:tc><w:p>` + run("违约金 10万元") + `</w:p></w:tc></w:tr></w:tbl>`
	src := buildDocx(t, tbl+para(run("正文")), nil)
	res := mustApply(t, src, Plan{Edits: []Edit{{Para: 2, Find: "10万元", Replace: str("1万元"), Comment: "过高"}}})
	if len(res.Skipped) != 0 {
		t.Fatalf("%+v", res.Skipped)
	}
	eq(t, "accepted", texts(t, res.Docx, false), []string{"项目", "违约金 1万元", "正文"})
	eq(t, "rejected", texts(t, res.Docx, true), []string{"项目", "违约金 10万元", "正文"})
}

func TestExistingCommentsPartIsExtended(t *testing.T) {
	extra := map[string]string{
		"word/comments.xml": `<?xml version="1.0" encoding="UTF-8"?><w:comments xmlns:w="` + nsW + `"><w:comment w:id="41" w:author="李四" w:date="2026-01-01T00:00:00Z"><w:p><w:r><w:t>旧批注</w:t></w:r></w:p></w:comment></w:comments>`,
	}
	body := para(`<w:commentRangeStart w:id="41"/>`+run("已有批注的文字")+`<w:commentRangeEnd w:id="41"/><w:r><w:commentReference w:id="41"/></w:r>`) + para(run("另一段"))
	src := buildDocx(t, body, extra)
	res := mustApply(t, src, Plan{Edits: []Edit{{Para: 2, Find: "另一段", Replace: str("再一段"), Comment: "新批注"}}})
	f, _ := readZip(res.Docx)
	c := string(f.comments)
	if !strings.Contains(c, "旧批注") || !strings.Contains(c, "新批注") || strings.Count(c, "<w:comment ") != 2 {
		t.Errorf("comments.xml: %s", c)
	}
	for _, m := range idRe.FindAllStringSubmatch(c, -1) {
		if m[1] == "41" && strings.Count(c, `w:id="41"`) != 1 {
			t.Errorf("comment id 41 reused")
		}
	}
	checkComments(t, res.Docx)
}

func TestNewIDsNeverCollideWithExistingOnes(t *testing.T) {
	body := `<w:bookmarkStart w:id="900" w:name="b"/>` + para(run("甲")+`<w:ins w:id="1200" w:author="x"><w:r><w:t>乙丙</w:t></w:r></w:ins>`) + `<w:bookmarkEnd w:id="900"/>`
	src := buildDocx(t, body, nil)
	res := mustApply(t, src, Plan{Edits: []Edit{{Para: 1, Find: "乙", Replace: str("丁"), Comment: "c"}}})
	checkIDs(t, res.Docx)
	for _, m := range idRe.FindAllStringSubmatch(documentXML(t, res.Docx), -1) {
		if m[1] != "900" && m[1] != "1200" && len(m[1]) < 4 {
			t.Errorf("new id %s is not above the existing maximum", m[1])
		}
	}
}

func TestNothingToDoReturnsTheOriginalPackage(t *testing.T) {
	src := buildDocx(t, para(run("内容")), nil)
	res := mustApply(t, src, Plan{Edits: []Edit{{Para: 5, Find: "x", Replace: str("y")}}})
	if string(res.Docx) != string(src) || len(res.Skipped) != 1 {
		t.Errorf("expected the untouched original and one skip, got %d skips", len(res.Skipped))
	}
}

func TestUnsupportedInput(t *testing.T) {
	if _, err := Apply([]byte("not a zip"), Plan{}, testAuthor, fixedNow); err == nil {
		t.Error("expected an error for a non-zip")
	}
	other := strings.Replace(string(buildDocx(t, para(run("x")), nil)), "", "", 1)
	_ = other
	bad := buildDocx(t, para(run("x")), map[string]string{
		"word/document.xml": `<?xml version="1.0"?><doc:document xmlns:doc="` + nsW + `"><doc:body><doc:p/></doc:body></doc:document>`,
	})
	if _, err := Apply(bad, Plan{}, testAuthor, fixedNow); err == nil {
		t.Error("a document that does not bind the w: prefix must be refused")
	}
}

func TestParagraphsListing(t *testing.T) {
	body := para(run("第一段")) + para("") + para(run("  ")) + para(run("第二")+`<w:del w:id="1" w:author="x"><w:r><w:delText>被删</w:delText></w:r></w:del>`+run("段"))
	ps, err := Paragraphs(buildDocx(t, body, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 || ps[0].Index != 1 || ps[0].Text != "第一段" || ps[1].Index != 2 || ps[1].Text != "第二段" {
		t.Errorf("%+v", ps)
	}
}
