package redline

import (
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const alphabet = "甲乙丙丁戊己庚辛壬癸，。 0123abc"

func randText(r *rand.Rand, min, max int) string {
	n := min + r.Intn(max-min+1)
	rs := []rune(alphabet)
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteRune(rs[r.Intn(len(rs))])
	}
	return b.String()
}

var rPrs = []string{"", "<w:rPr><w:b/></w:rPr>", `<w:rPr><w:i/><w:color w:val="FF0000"/></w:rPr>`, `<w:rPr><w:rFonts w:ascii="Arial"/><w:sz w:val="28"/></w:rPr>`}

func esc(s string) string { return escape(s) }

func mkRun(r *rand.Rand, text string) string {
	open := "<w:r>"
	if r.Intn(4) == 0 {
		open = `<w:r w:rsidR="00A1B2C3">`
	}
	return open + rPrs[r.Intn(len(rPrs))] + `<w:t xml:space="preserve">` + esc(text) + `</w:t></w:r>`
}

// randParagraph returns paragraph XML and the text it currently reads as.
func randParagraph(r *rand.Rand, id *int) (xmlStr, text string) {
	var x, t strings.Builder
	if r.Intn(3) == 0 {
		x.WriteString(`<w:pPr><w:jc w:val="both"/></w:pPr>`)
	}
	for i, n := 0, 1+r.Intn(6); i < n; i++ {
		switch k := r.Intn(10); {
		case k < 5:
			s := randText(r, 1, 7)
			x.WriteString(mkRun(r, s))
			t.WriteString(s)
		case k == 5, k == 6:
			*id++
			var inner strings.Builder
			for j, m := 0, 1+r.Intn(2); j < m; j++ {
				s := randText(r, 1, 5)
				inner.WriteString(mkRun(r, s))
				t.WriteString(s)
			}
			fmt.Fprintf(&x, `<w:ins w:id="%d" w:author="张三" w:date="2026-01-01T00:00:00Z">%s</w:ins>`, *id, inner.String())
		case k == 7:
			*id++
			fmt.Fprintf(&x, `<w:del w:id="%d" w:author="张三" w:date="2026-01-01T00:00:00Z"><w:r><w:delText xml:space="preserve">%s</w:delText></w:r></w:del>`, *id, esc(randText(r, 1, 4)))
		case k == 8:
			s := randText(r, 1, 4)
			fmt.Fprintf(&x, `<w:hyperlink r:id="rId7"><w:r><w:t xml:space="preserve">%s</w:t></w:r></w:hyperlink>`, esc(s))
			t.WriteString(s)
		default:
			s := randText(r, 1, 4)
			fmt.Fprintf(&x, `<w:r><w:tab/><w:t xml:space="preserve">%s</w:t></w:r>`, esc(s))
			t.WriteString("\t" + s)
		}
		if r.Intn(5) == 0 {
			x.WriteString(`<w:proofErr w:type="spellStart"/>`)
		}
	}
	return `<w:p>` + x.String() + `</w:p>`, t.String()
}

func TestRandomisedRedlinesKeepEveryInvariant(t *testing.T) {
	var proposals, applied int
	reasons := map[string]int{}
	for seed := int64(1); seed <= soak(); seed++ {
		r := rand.New(rand.NewSource(seed))
		id := 100
		var body strings.Builder
		var orig []string
		for i, n := 0, 2+r.Intn(4); i < n; i++ {
			x, text := randParagraph(r, &id)
			if i == 1 && r.Intn(3) == 0 { // some paragraphs live in a table cell
				x = `<w:tbl><w:tr><w:tc>` + x + `</w:tc></w:tr></w:tbl>`
			}
			body.WriteString(x)
			if strings.TrimSpace(text) != "" {
				orig = append(orig, text)
			}
		}
		src := buildDocx(t, body.String(), nil)
		fail := func(format string, a ...any) {
			t.Helper()
			t.Fatalf("seed %d: %s\nbody: %s", seed, fmt.Sprintf(format, a...), body.String())
		}

		var plan Plan
		for i, n := 0, 1+r.Intn(5); i < n && len(orig) > 0; i++ {
			pi := r.Intn(len(orig))
			rs := []rune(orig[pi])
			a := r.Intn(len(rs))
			b := a + 1 + r.Intn(min(8, len(rs)-a))
			find := string(rs[a:b])
			e := Edit{Para: pi + 1, Find: find}
			switch r.Intn(5) {
			case 0: // comment only
			case 1:
				e.Replace = str(find) // identical
			case 2:
				e.Replace = str("")
			default:
				e.Replace = str(randText(r, 0, 8))
			}
			if r.Intn(2) == 0 {
				e.Comment = "批注" + randText(r, 1, 6)
			}
			plan.Edits = append(plan.Edits, e)
		}
		for i, n := 0, r.Intn(3); i < n && len(orig) > 0; i++ {
			in := Insertion{After: 1 + r.Intn(len(orig)), Text: "新增" + randText(r, 1, 8)}
			if r.Intn(2) == 0 {
				in.Comment = "补充"
			}
			plan.Insertions = append(plan.Insertions, in)
		}

		res, err := Apply(src, plan, testAuthor, fixedNow)
		if err != nil {
			fail("Apply: %v", err)
		}
		proposals += len(plan.Edits) + len(plan.Insertions)
		applied += res.AppliedCount()
		for _, s := range res.Skipped {
			if strings.Contains(s.Reason, "内部校验") {
				fail("internal verification failed (an emitter bug): %+v\nplan: %+v", s, plan)
			}
			reasons[s.Reason]++
		}

		// Rejecting the AI's changes gives back the original, always.
		got := texts(t, res.Docx, true)
		if strings.Join(got, "\x00") != strings.Join(orig, "\x00") {
			fail("rejected view differs:\n got  %q\n want %q\nplan: %+v", got, orig, plan)
		}

		// Accepting them gives the original with exactly the applied edits.
		want := append([]string(nil), orig...)
		type pos struct {
			a, b int
			repl string
		}
		byPara := map[int][]pos{}
		inserts := map[int][]string{}
		for _, it := range res.Applied {
			switch it.Kind {
			case "edit":
				e := plan.Edits[it.Index]
				a, b, st := locate([]rune(orig[e.Para-1]), []rune(e.Find))
				if st != locOne {
					fail("an applied edit cannot be located: %+v", e)
				}
				if e.Replace != nil && string([]rune(orig[e.Para-1])[a:b]) != *e.Replace {
					byPara[e.Para] = append(byPara[e.Para], pos{a, b, *e.Replace})
				}
			case "insertion":
				in := plan.Insertions[it.Index]
				inserts[in.After] = append(inserts[in.After], strings.TrimSpace(in.Text))
			}
		}
		for p, ps := range byPara {
			rs := []rune(want[p-1])
			// Applied edits never overlap, so replacing from the right keeps offsets valid.
			sort.Slice(ps, func(i, j int) bool { return ps[i].a > ps[j].a })
			for _, e := range ps {
				rs = append(rs[:e.a:e.a], append([]rune(e.repl), rs[e.b:]...)...)
			}
			want[p-1] = string(rs)
		}
		var wantAll []string
		for i, txt := range want {
			if strings.TrimSpace(txt) != "" {
				wantAll = append(wantAll, txt)
			}
			wantAll = append(wantAll, inserts[i+1]...)
		}
		// A paragraph emptied by a deletion drops out of the numbered view in both lists.
		gotAcc := texts(t, res.Docx, false)
		if strings.Join(gotAcc, "\x00") != strings.Join(wantAll, "\x00") {
			fail("accepted view differs:\n got  %q\n want %q\nplan: %+v\napplied: %+v", gotAcc, wantAll, plan, res.Applied)
		}

		checkIDs(t, res.Docx)
		checkComments(t, res.Docx)
	}
	if applied*10 < proposals*4 {
		t.Errorf("only %d of %d proposals were applied; the test would be vacuous", applied, proposals)
	}
	t.Logf("%d/%d proposals applied; skip reasons: %v", applied, proposals, reasons)
}

// soak lets a longer run be requested with REDLINE_SOAK=<n>; CI uses the default.
func soak() int64 {
	if v := os.Getenv("REDLINE_SOAK"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return int64(n)
		}
	}
	return 500
}
