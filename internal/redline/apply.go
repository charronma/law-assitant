package redline

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Edit replaces Find (copied verbatim from paragraph Para) with Replace. When
// Replace is nil or equal to Find the text is only annotated with Comment.
type Edit struct {
	Para    int
	Find    string
	Replace *string
	Comment string
}

// Insertion adds a new paragraph after paragraph After.
type Insertion struct {
	After   int
	Text    string
	Comment string
}

// Plan is a set of proposed revisions.
type Plan struct {
	Edits      []Edit
	Insertions []Insertion
}

// Item identifies one proposal of a Plan; Reason says why it was skipped.
type Item struct {
	Kind   string // "edit" or "insertion"
	Index  int    // position in Plan.Edits / Plan.Insertions
	Para   int
	Reason string
}

// Result is the revised document and what happened to each proposal.
type Result struct {
	Docx    []byte
	Applied []Item
	Skipped []Item
}

// AppliedCount returns how many proposals were applied.
func (r Result) AppliedCount() int { return len(r.Applied) }

// span is one resolved edit inside one paragraph. Offsets are rune offsets into
// the paragraph's current text.
type span struct {
	index int // Plan.Edits index

	oa, ob int // the original text the edit targeted, before trimming

	a, b   int    // change region; a == b is a pure insertion
	repl   []rune // text to insert at b
	change bool   // there is something to delete or insert

	ca, cb     int // comment range (contains [a, b])
	comment    string
	hasComment bool
	id         int // comment id, assigned when emitted

	zeroOwner *node // for pure insertions: the run the insertion is attached to
	zeroStart bool  // true: emit before the owner's piece starting at a; false: after the piece ending at a

	rPr  string // run properties to give inserted text
	done bool   // comment start emitted

	firedFlag bool // insertion emitted
}

// isIns reports a pure insertion (nothing deleted).
func (s *span) isIns() bool { return s.change && s.a == s.b }

type comment struct {
	id   int
	text string
}

type gen struct{ n int }

func (g *gen) next() int { g.n++; return g.n }

// Apply writes plan into docx as tracked changes and comments by author.
//
// Each affected paragraph is rebuilt and then checked: read with the AI's
// changes accepted its text must equal the intended text, and read with them
// rejected it must equal the original. A paragraph that fails the check is left
// untouched and its proposals are reported as skipped, so the result is never a
// document the user cannot trust.
func Apply(docx []byte, plan Plan, author string, now time.Time) (Result, error) {
	files, err := readZip(docx)
	if err != nil {
		return Result{}, err
	}
	doc := files.document
	if !bytes.Contains(doc[:min(len(doc), 8192)], []byte(`xmlns:w="`+nsW+`"`)) {
		return Result{}, fmt.Errorf("%w: the w: namespace prefix is not declared on the root element", ErrUnsupported)
	}
	root, err := parseXML(doc)
	if err != nil {
		return Result{}, err
	}
	paras := numbered(readParagraphs(root, view{}))

	g := &gen{n: maxID(root, files.comments)}
	date := now.UTC().Format("2006-01-02T15:04:05Z")
	b := &builder{doc: doc, root: root, author: author, date: date, g: g}

	var res Result
	skip := func(kind string, idx, para int, reason string) {
		res.Skipped = append(res.Skipped, Item{Kind: kind, Index: idx, Para: para, Reason: reason})
	}

	// 1. Resolve edits to spans, per paragraph.
	spans := map[int][]*span{} // paragraph number -> spans
	for i, e := range plan.Edits {
		if e.Para < 1 || e.Para > len(paras) {
			skip("edit", i, e.Para, "段落编号不存在")
			continue
		}
		s, reason := resolveEdit(paras[e.Para-1], e, i)
		if reason != "" {
			skip("edit", i, e.Para, reason)
			continue
		}
		clash := false
		for _, o := range spans[e.Para] {
			if conflicts(o, s) {
				clash = true
				break
			}
		}
		if clash {
			skip("edit", i, e.Para, "与同一段落中的另一处修改位置重叠")
			continue
		}
		spans[e.Para] = append(spans[e.Para], s)
	}
	insertions := map[int][]int{} // paragraph number -> Plan.Insertions indexes
	for i, in := range plan.Insertions {
		switch {
		case in.After < 1 || in.After > len(paras):
			skip("insertion", i, in.After, "段落编号不存在")
		case strings.TrimSpace(in.Text) == "":
			skip("insertion", i, in.After, "新增内容为空")
		default:
			insertions[in.After] = append(insertions[in.After], i)
		}
	}

	// 2. Rebuild each affected paragraph, verifying it.
	type splice struct {
		start, end int
		xml        string
	}
	var splices []splice
	var comments []comment
	var covered [][2]int // byte ranges of rewritten paragraphs, to refuse nesting

	var nums []int
	seen := map[int]bool{}
	for n := range spans {
		seen[n] = true
	}
	for n := range insertions {
		seen[n] = true
	}
	for n := range seen {
		nums = append(nums, n)
	}
	sort.Ints(nums)

	for _, n := range nums {
		p := paras[n-1]
		sp := spans[n]
		sort.Slice(sp, func(i, j int) bool {
			if sp[i].ca != sp[j].ca {
				return sp[i].ca < sp[j].ca
			}
			return sp[i].a < sp[j].a
		})

		fail := func(reason string) {
			for _, s := range sp {
				skip("edit", s.index, n, reason)
			}
			for _, i := range insertions[n] {
				skip("insertion", i, n, reason)
			}
		}
		if nested(covered, p.n) {
			fail("该段落位于另一处已修改的内容内部（如文本框），无法安全修改")
			continue
		}

		var newXML strings.Builder
		var paraComments []comment
		start, end := p.n.start, p.n.end
		if len(sp) > 0 {
			x, cs, err := b.rewrite(p, sp)
			if err == nil {
				err = b.verify(x, p, sp)
			}
			if err != nil {
				fail("内部校验未通过：" + err.Error())
				continue
			}
			newXML.WriteString(x)
			paraComments = append(paraComments, cs...)
		} else {
			newXML.WriteString(string(doc[start:end]))
		}

		var insXML []string
		var insComments []comment
		for _, i := range insertions[n] {
			x, cs, err := b.newParagraph(p, plan.Insertions[i])
			if err == nil {
				err = b.verifyInserted(x, plan.Insertions[i].Text)
			}
			if err != nil {
				skip("insertion", i, n, "内部校验未通过："+err.Error())
				continue
			}
			insXML = append(insXML, x)
			insComments = append(insComments, cs...)
			res.Applied = append(res.Applied, Item{Kind: "insertion", Index: i, Para: n})
		}
		for _, x := range insXML {
			newXML.WriteString(x)
		}
		if len(sp) == 0 && len(insXML) == 0 {
			continue
		}
		for _, s := range sp {
			res.Applied = append(res.Applied, Item{Kind: "edit", Index: s.index, Para: n})
		}
		comments = append(comments, paraComments...)
		comments = append(comments, insComments...)
		splices = append(splices, splice{start, end, newXML.String()})
		covered = append(covered, [2]int{start, end})
	}

	if len(splices) == 0 {
		res.Docx = docx
		return res, nil
	}

	// 3. Splice back to front so earlier offsets stay valid.
	sort.Slice(splices, func(i, j int) bool { return splices[i].start > splices[j].start })
	out := append([]byte(nil), doc...)
	for _, s := range splices {
		out = append(out[:s.start], append([]byte(s.xml), out[s.end:]...)...)
	}

	replace := map[string][]byte{"word/document.xml": out}
	if len(comments) > 0 {
		cx, err := commentsPart(files.comments, comments, author, date)
		if err != nil {
			return Result{}, err
		}
		replace["word/comments.xml"] = cx
		if err := files.withCommentsPart(replace); err != nil {
			return Result{}, err
		}
	}
	if _, err := parseXML(out); err != nil {
		return Result{}, fmt.Errorf("redline produced malformed XML: %w", err)
	}
	res.Docx, err = files.write(replace)
	if err != nil {
		return Result{}, err
	}
	sort.Slice(res.Applied, func(i, j int) bool {
		if res.Applied[i].Kind != res.Applied[j].Kind {
			return res.Applied[i].Kind < res.Applied[j].Kind
		}
		return res.Applied[i].Index < res.Applied[j].Index
	})
	return res, nil
}

func nested(covered [][2]int, n *node) bool {
	for _, c := range covered {
		if n.start >= c[0] && n.end <= c[1] {
			return true
		}
		if c[0] >= n.start && c[1] <= n.end {
			return true
		}
	}
	return false
}

// maxID finds the largest numeric w:id in the package so new ids never collide.
func maxID(root *node, comments []byte) int {
	m := 0
	var walk func(*node)
	walk = func(n *node) {
		if v, ok := n.attrs["id"]; ok {
			if i, err := strconv.Atoi(v); err == nil && i > m {
				m = i
			}
		}
		for _, k := range n.kids {
			walk(k)
		}
	}
	walk(root)
	if comments != nil {
		if croot, err := parseXML(comments); err == nil {
			walk(croot)
		}
	}
	return m
}

// ---- resolving an edit to a span ------------------------------------------

func resolveEdit(p *paragraph, e Edit, index int) (*span, string) {
	find := []rune(e.Find)
	if len(strings.TrimSpace(e.Find)) == 0 {
		return nil, "原文（find）为空"
	}
	a0, b0, status := locate(p.text, find)
	switch status {
	case locNone:
		return nil, "未在该段落中找到这段原文"
	case locMany:
		return nil, "这段原文在该段落中出现多次，无法唯一定位"
	}
	actual := p.text[a0:b0]

	s := &span{index: index, comment: strings.TrimSpace(e.Comment)}
	s.hasComment = s.comment != ""
	s.ca, s.cb = a0, b0
	s.oa, s.ob = a0, b0

	if e.Replace == nil || string(actual) == *e.Replace {
		if !s.hasComment {
			return nil, "没有修改内容也没有批注"
		}
		// Comment only.
		if !emitStart(p, s.ca) || !emitEnd(p, s.cb) {
			return nil, "无法确定批注位置"
		}
		s.a, s.b = a0, a0
		return s, ""
	}

	repl := []rune(*e.Replace)
	// Mark only what really changes: trim the common prefix and suffix.
	pre := 0
	for pre < len(actual) && pre < len(repl) && actual[pre] == repl[pre] {
		pre++
	}
	suf := 0
	for suf < len(actual)-pre && suf < len(repl)-pre && actual[len(actual)-1-suf] == repl[len(repl)-1-suf] {
		suf++
	}
	s.a, s.b = a0+pre, b0-suf
	s.repl = repl[pre : len(repl)-suf]
	s.change = true

	if s.a < s.b {
		// Everything being deleted must be editable plain text.
		for _, sg := range p.segs {
			if sg.b <= s.a || sg.a >= s.b {
				continue
			}
			if !sg.editable {
				return nil, "修改范围包含无法编辑的内容（如制表符、超链接、域或已有修订）"
			}
		}
		if covered(p, s.a, s.b) != s.b-s.a {
			return nil, "修改范围包含无法编辑的内容"
		}
	} else {
		// Pure insertion at s.a.
		if owner := endOwner(p, s.a); owner != nil {
			s.zeroOwner, s.zeroStart = owner, false
		} else if owner := startOwner(p, s.a); owner != nil {
			s.zeroOwner, s.zeroStart = owner, true
		} else {
			return nil, "插入位置不在可编辑的文字上"
		}
	}

	switch {
	case s.isIns():
		s.ca, s.cb = s.a, s.a // the comment is anchored by the insertion itself
	case !s.hasComment:
		s.ca, s.cb = s.a, s.b
	default:
		// The comment may span the whole original text, if its ends can be anchored.
		if !emitStart(p, s.ca) {
			s.ca = s.a
		}
		if !emitEnd(p, s.cb) {
			s.cb = s.b
		}
	}
	return s, ""
}

func covered(p *paragraph, a, b int) int {
	n := 0
	for _, sg := range p.segs {
		if !sg.editable {
			continue
		}
		lo, hi := max(sg.a, a), min(sg.b, b)
		if hi > lo {
			n += hi - lo
		}
	}
	return n
}

// emitStart reports whether something can be emitted just before offset x.
func emitStart(p *paragraph, x int) bool { return startOwner(p, x) != nil }

// emitEnd reports whether something can be emitted just after offset x.
func emitEnd(p *paragraph, x int) bool { return endOwner(p, x) != nil }

func startOwner(p *paragraph, x int) *node {
	for _, sg := range p.segs {
		if sg.editable && sg.a <= x && x < sg.b {
			return sg.run
		}
	}
	return nil
}

func endOwner(p *paragraph, x int) *node {
	for _, sg := range p.segs {
		if sg.editable && sg.a < x && x <= sg.b {
			return sg.run
		}
	}
	return nil
}

func conflicts(x, y *span) bool {
	// Two edits aimed at overlapping original text are contradictory, however
	// small the part that actually changes.
	if x.oa < y.ob && y.oa < x.ob {
		return true
	}
	if x.isIns() && y.isIns() && x.a == y.a {
		return true
	}
	lo, hi := max(x.ca, y.ca), min(x.cb, y.cb)
	if lo < hi {
		return true
	}
	// A pure insertion strictly inside another span's change region.
	for _, p := range [][2]*span{{x, y}, {y, x}} {
		if p[0].isIns() && p[1].change && p[1].a < p[0].a && p[0].a < p[1].b {
			return true
		}
	}
	return false
}

const (
	locNone = iota
	locOne
	locMany
)

// locate finds find inside text exactly once; if it is not found verbatim it
// retries ignoring all whitespace (models often normalise spacing).
func locate(text, find []rune) (a, b, status int) {
	if i, n := indexAll(text, find); n == 1 {
		return i, i + len(find), locOne
	} else if n > 1 {
		return 0, 0, locMany
	}
	nt, tmap := stripSpace(text)
	nf, _ := stripSpace(find)
	if len(nf) == 0 {
		return 0, 0, locNone
	}
	i, n := indexAll(nt, nf)
	switch {
	case n == 1:
		return tmap[i], tmap[i+len(nf)-1] + 1, locOne
	case n > 1:
		return 0, 0, locMany
	}
	return 0, 0, locNone
}

func indexAll(hay, needle []rune) (first, count int) {
	first = -1
	for i := 0; i+len(needle) <= len(hay); i++ {
		match := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			if first < 0 {
				first = i
			}
			count++
		}
	}
	return
}

func stripSpace(in []rune) (out []rune, origIndex []int) {
	for i, r := range in {
		if unicode.IsSpace(r) || r == ' ' || r == '　' {
			continue
		}
		out = append(out, r)
		origIndex = append(origIndex, i)
	}
	return
}

// ---- XML emission ------------------------------------------------------------

type builder struct {
	doc    []byte
	root   *node
	author string
	date   string
	g      *gen
}

func (b *builder) attrs(id int) string {
	return fmt.Sprintf(` w:id="%d" w:author="%s" w:date="%s"`, id, escape(b.author), b.date)
}

func (b *builder) raw(n *node) string { return string(b.doc[n.start:n.end]) }

func rPrOf(b *builder, run *node) string {
	if k := run.child("rPr"); k != nil {
		return b.raw(k)
	}
	return ""
}

// textContent renders text as run content (w:t, with tabs and line breaks).
func textContent(text []rune, elem string) string {
	var out strings.Builder
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out.WriteString(`<w:` + elem + ` xml:space="preserve">` + escape(string(cur)) + `</w:` + elem + `>`)
			cur = nil
		}
	}
	for _, r := range text {
		switch r {
		case '\n':
			flush()
			out.WriteString(`<w:br/>`)
		case '\t':
			flush()
			out.WriteString(`<w:tab/>`)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return out.String()
}

func (b *builder) insertedRun(rPr string, text []rune) string {
	return `<w:ins` + b.attrs(b.g.next()) + `><w:r>` + rPr + textContent(text, "t") + `</w:r></w:ins>`
}

func commentEnd(id int) string {
	return fmt.Sprintf(`<w:commentRangeEnd w:id="%d"/><w:r><w:commentReference w:id="%d"/></w:r>`, id, id)
}

// rewrite rebuilds paragraph p with every span applied.
func (b *builder) rewrite(p *paragraph, spans []*span) (string, []comment, error) {
	for _, s := range spans {
		if s.hasComment {
			s.id = b.g.next()
		}
	}
	segOf := map[*node]*segment{}
	for i := range p.segs {
		if p.segs[i].editable {
			segOf[p.segs[i].run] = &p.segs[i]
		}
	}
	touched := func(sg *segment) bool {
		for _, s := range spans {
			if s.isIns() {
				if s.zeroOwner == sg.run {
					return true
				}
				continue
			}
			lo, hi := max(sg.a, s.ca), min(sg.b, s.cb)
			if lo < hi {
				return true
			}
		}
		return false
	}

	var out strings.Builder
	pos := p.n.start
	emitGap := func(to int) {
		out.Write(b.doc[pos:to])
	}
	for _, k := range p.n.kids {
		emitGap(k.start)
		pos = k.end
		switch {
		case k.is("r") && segOf[k] != nil && touched(segOf[k]):
			b.emitRun(&out, segOf[k], spans)
		case k.is("ins"):
			var any bool
			for _, r := range k.kids {
				if sg := segOf[r]; sg != nil && touched(sg) {
					any = true
				}
			}
			if !any {
				out.Write(b.doc[k.start:k.end])
				continue
			}
			// Re-emit the wrapper run by run, so each can be split independently.
			for _, r := range k.kids {
				if sg := segOf[r]; sg != nil && touched(sg) {
					b.emitRun(&out, sg, spans)
				} else {
					out.WriteString(b.wrap(k, b.raw(r)))
				}
			}
		default:
			out.Write(b.doc[k.start:k.end])
		}
	}
	out.Write(b.doc[pos:p.n.end])

	var cs []comment
	for _, s := range spans {
		if s.hasComment {
			cs = append(cs, comment{id: s.id, text: s.comment})
		}
	}
	return out.String(), cs, nil
}

// wrap re-wraps inner in a copy of the (someone else's) w:ins element w.
func (b *builder) wrap(w *node, inner string) string {
	return `<w:ins w:id="` + strconv.Itoa(b.g.next()) + `" w:author="` + escape(w.attrs["author"]) +
		`" w:date="` + escape(w.attrs["date"]) + `">` + inner + `</w:ins>`
}

// emitRun writes one editable run, cut at every span boundary, with deletions,
// insertions and comment anchors in place.
func (b *builder) emitRun(out *strings.Builder, sg *segment, spans []*span) {
	cutSet := map[int]bool{}
	for _, s := range spans {
		for _, c := range []int{s.ca, s.cb, s.a, s.b} {
			if c > sg.a && c < sg.b {
				cutSet[c] = true
			}
		}
	}
	points := []int{sg.a, sg.b}
	for c := range cutSet {
		points = append(points, c)
	}
	sort.Ints(points)
	// de-dup (sg.a / sg.b are never in cutSet, so points are unique)

	openTag := string(b.doc[sg.run.start:sg.run.openEnd])
	rPr := rPrOf(b, sg.run)
	wrap := func(inner string) string {
		if sg.wrapper != nil {
			return b.wrap(sg.wrapper, inner)
		}
		return inner
	}

	for i := 0; i+1 < len(points); i++ {
		x, y := points[i], points[i+1]
		text := sg.text[x-sg.a : y-sg.a]

		// Events before this piece.
		for _, s := range spans {
			if s.hasComment && s.ca == x && !s.isIns() && !s.done {
				fmt.Fprintf(out, `<w:commentRangeStart w:id="%d"/>`, s.id)
				s.done = true
			}
			if s.isIns() && s.zeroStart && s.zeroOwner == sg.run && s.a == x {
				b.emitInsertion(out, s, rPr)
			}
		}

		// The piece itself: deleted if inside a change region.
		deleted := false
		for _, s := range spans {
			if s.change && !s.isIns() && s.a <= x && y <= s.b {
				deleted = true
				if s.rPr == "" {
					s.rPr = rPr
				}
			}
		}
		if deleted {
			out.WriteString(wrap(`<w:del` + b.attrs(b.g.next()) + `>` + openTag + rPr + textContent(text, "delText") + `</w:r></w:del>`))
		} else {
			out.WriteString(wrap(openTag + rPr + textContent(text, "t") + `</w:r>`))
		}

		// Events after this piece: insertions first, then comment ends.
		for _, s := range spans {
			if s.change && !s.isIns() && s.b == y && !s.fired() {
				if len(s.repl) > 0 {
					out.WriteString(b.insertedRun(s.rPr, s.repl))
				}
				s.markFired()
			}
			if s.isIns() && !s.zeroStart && s.zeroOwner == sg.run && s.a == y {
				b.emitInsertion(out, s, rPr)
			}
		}
		for _, s := range spans {
			if s.hasComment && s.cb == y && !s.isIns() {
				out.WriteString(commentEnd(s.id))
			}
		}
	}
}

// emitInsertion writes a pure insertion together with its own comment anchors.
func (b *builder) emitInsertion(out *strings.Builder, s *span, rPr string) {
	if s.hasComment {
		fmt.Fprintf(out, `<w:commentRangeStart w:id="%d"/>`, s.id)
	}
	out.WriteString(b.insertedRun(rPr, s.repl))
	if s.hasComment {
		out.WriteString(commentEnd(s.id))
	}
	s.markFired()
}

func (s *span) fired() bool { return s.firedFlag }
func (s *span) markFired()  { s.firedFlag = true }

// newParagraph builds a tracked-inserted paragraph that follows paragraph p
// and shares its paragraph properties (so numbering continues).
func (b *builder) newParagraph(p *paragraph, in Insertion) (string, []comment, error) {
	insMark := `<w:ins` + b.attrs(b.g.next()) + `/>`
	var ppr string
	if pp := p.n.child("pPr"); pp != nil {
		var inner strings.Builder
		pos := pp.openEnd
		hadRPr := false
		for _, k := range pp.kids {
			inner.Write(b.doc[pos:k.start])
			pos = k.end
			switch {
			case k.is("sectPr"), k.is("pPrChange"):
				// belongs to the original paragraph only
			case k.is("rPr"):
				hadRPr = true
				// our paragraph-mark insertion goes first inside rPr
				if k.openEnd == k.end { // <w:rPr/>
					inner.WriteString(`<w:rPr>` + insMark + `</w:rPr>`)
					break
				}
				open := string(b.doc[k.start:k.openEnd])
				inner.WriteString(open + insMark + string(b.doc[k.openEnd:k.end]))
			default:
				inner.Write(b.doc[k.start:k.end])
			}
		}
		// pPr's closing tag: everything after the last child
		closeIdx := bytes.LastIndex(b.doc[pp.openEnd:pp.end], []byte("</"))
		if closeIdx >= 0 {
			inner.Write(b.doc[pos : pp.openEnd+closeIdx])
		}
		if !hadRPr {
			inner.WriteString(`<w:rPr>` + insMark + `</w:rPr>`)
		}
		ppr = string(b.doc[pp.start:pp.openEnd]) + inner.String() + `</w:pPr>`
		if pp.openEnd == pp.end { // <w:pPr/>
			ppr = `<w:pPr><w:rPr>` + insMark + `</w:rPr></w:pPr>`
		}
	} else {
		ppr = `<w:pPr><w:rPr>` + insMark + `</w:rPr></w:pPr>`
	}

	rPr := ""
	for _, sg := range p.segs {
		if sg.run != nil {
			if r := rPrOf(b, sg.run); r != "" {
				rPr = r
				break
			}
		}
	}

	var body strings.Builder
	var cs []comment
	text := []rune(strings.TrimSpace(in.Text))
	if c := strings.TrimSpace(in.Comment); c != "" {
		id := b.g.next()
		fmt.Fprintf(&body, `<w:commentRangeStart w:id="%d"/>`, id)
		body.WriteString(b.insertedRun(rPr, text))
		body.WriteString(commentEnd(id))
		cs = append(cs, comment{id: id, text: c})
	} else {
		body.WriteString(b.insertedRun(rPr, text))
	}
	return `<w:p>` + ppr + body.String() + `</w:p>`, cs, nil
}

// ---- verification -------------------------------------------------------------

// standalone wraps paragraph XML in a copy of the document's root element so it
// parses with the same namespace declarations.
func (b *builder) standalone(paraXML string) []byte {
	open := b.doc[b.root.start:b.root.openEnd]
	closeIdx := bytes.LastIndex(b.doc[:b.root.end], []byte("</"))
	closeTag := b.doc[closeIdx:b.root.end]
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	buf.Write(open)
	buf.WriteString(`<w:body>`)
	buf.WriteString(paraXML)
	buf.WriteString(`</w:body>`)
	buf.Write(closeTag)
	return buf.Bytes()
}

func (b *builder) readFirst(paraXML string, rejectAI bool) (string, error) {
	root, err := parseXML(b.standalone(paraXML))
	if err != nil {
		return "", err
	}
	ps := readParagraphs(root, view{author: b.author, rejectAI: rejectAI})
	if len(ps) == 0 {
		return "", fmt.Errorf("段落丢失")
	}
	return string(ps[0].text), nil
}

// verify checks a rewritten paragraph against its intended text.
func (b *builder) verify(newXML string, p *paragraph, spans []*span) error {
	want := []rune(string(p.text))
	ordered := append([]*span(nil), spans...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].a != ordered[j].a {
			return ordered[i].a > ordered[j].a
		}
		return ordered[i].b > ordered[j].b
	})
	for _, s := range ordered {
		if !s.change {
			continue
		}
		want = append(want[:s.a:s.a], append(append([]rune(nil), s.repl...), want[s.b:]...)...)
	}
	got, err := b.readFirst(newXML, false)
	if err != nil {
		return err
	}
	if got != string(want) {
		return fmt.Errorf("修改后的文字与预期不一致")
	}
	back, err := b.readFirst(newXML, true)
	if err != nil {
		return err
	}
	if back != string(p.text) {
		return fmt.Errorf("拒绝修订后无法还原原文")
	}
	return nil
}

func (b *builder) verifyInserted(newXML, text string) error {
	got, err := b.readFirst(newXML, false)
	if err != nil {
		return err
	}
	if strings.TrimSpace(got) != strings.TrimSpace(text) {
		return fmt.Errorf("新增段落的文字与预期不一致")
	}
	back, err := b.readFirst(newXML, true)
	if err != nil {
		return err
	}
	if strings.TrimSpace(back) != "" {
		return fmt.Errorf("拒绝修订后新增段落没有消失")
	}
	return nil
}

// ---- comments part ------------------------------------------------------------

func commentsPart(existing []byte, cs []comment, author, date string) ([]byte, error) {
	var items strings.Builder
	for _, c := range cs {
		items.WriteString(fmt.Sprintf(`<w:comment w:id="%d" w:author="%s" w:date="%s" w:initials="AI"><w:p><w:r><w:annotationRef/></w:r><w:r>%s</w:r></w:p></w:comment>`,
			c.id, escape(author), date, textContent([]rune(c.text), "t")))
	}
	if existing == nil {
		return []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<w:comments xmlns:w="` + nsW + `">` + items.String() + `</w:comments>`), nil
	}
	i := bytes.LastIndex(existing, []byte("</w:comments>"))
	if i < 0 {
		return nil, fmt.Errorf("%w: existing comments.xml is not in the expected form", ErrUnsupported)
	}
	out := append([]byte(nil), existing[:i]...)
	out = append(out, items.String()...)
	return append(out, existing[i:]...), nil
}

// escape XML-escapes text and drops characters XML 1.0 forbids.
func escape(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || r >= 0x20 && r != 0xFFFE && r != 0xFFFF && (r < 0xD800 || r > 0xDFFF) {
			return r
		}
		return -1
	}, s)
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}
