package redline

import (
	"strings"
	"testing"
)

func TestParsePlanVariants(t *testing.T) {
	good := `{"summary":"总结","edits":[{"para":3,"find":"甲","replace":"乙","comment":"c"},{"para":"P4","find":"丙"}],"insertions":[{"after":"5","text":"新条款","comment":"x"}]}`
	cases := map[string]string{
		"plain":       good,
		"fenced":      "```json\n" + good + "\n```",
		"with prose":  "好的，以下是修改清单：\n" + good + "\n希望对你有帮助。",
		"upper fence": "```JSON\n" + good + "```",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			p, sum, err := ParsePlan(in)
			if err != nil {
				t.Fatal(err)
			}
			if sum != "总结" || len(p.Edits) != 2 || len(p.Insertions) != 1 {
				t.Fatalf("%s %+v", sum, p)
			}
			if p.Edits[0].Para != 3 || p.Edits[1].Para != 4 || p.Insertions[0].After != 5 {
				t.Errorf("paragraph numbers not coerced: %+v", p)
			}
			if p.Edits[0].Replace == nil || *p.Edits[0].Replace != "乙" {
				t.Errorf("replace lost")
			}
			if p.Edits[1].Replace != nil {
				t.Errorf("an omitted replace must stay nil (comment only)")
			}
		})
	}
}

func TestParsePlanDistinguishesEmptyReplaceFromMissing(t *testing.T) {
	p, _, err := ParsePlan(`{"edits":[{"para":1,"find":"a","replace":""},{"para":1,"find":"b","replace":null},{"para":1,"find":"c"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if p.Edits[0].Replace == nil || *p.Edits[0].Replace != "" {
		t.Error("an empty string means delete")
	}
	if p.Edits[1].Replace != nil || p.Edits[2].Replace != nil {
		t.Error("null / missing means comment only")
	}
}

func TestParsePlanSalvagesTruncatedOutput(t *testing.T) {
	cut := `{"summary":"主要风险","edits":[{"para":1,"find":"甲","replace":"乙","comment":"一"},{"para":2,"find":"丙","replace":"丁","comment":"二"},{"para":3,"find":"戊","replace":"己","comm`
	p, sum, err := ParsePlan(cut)
	if err != nil {
		t.Fatal(err)
	}
	if sum != "主要风险" || len(p.Edits) != 2 {
		t.Fatalf("want the 2 complete edits, got %s %+v", sum, p.Edits)
	}
}

func TestParsePlanRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "抱歉，我无法完成", `{"edits":"not an array"}`, "{{{"} {
		if _, _, err := ParsePlan(in); err == nil {
			t.Errorf("%q should be an error", in)
		}
	}
}

func TestBuildUserPrompt(t *testing.T) {
	paras := []Paragraph{{1, "第一段"}, {2, "第二段"}, {3, "第三段很长很长"}}
	p, trunc := BuildUserPrompt("我是甲方", paras, 0)
	if trunc || !strings.Contains(p, "[P1] 第一段") || !strings.Contains(p, "[P3] 第三段很长很长") || !strings.Contains(p, "我是甲方") {
		t.Errorf("%q", p)
	}
	p, trunc = BuildUserPrompt("", paras, 7)
	if !trunc || strings.Contains(p, "[P3]") || !strings.Contains(p, "[P2]") || !strings.Contains(p, "我方") {
		t.Errorf("truncated=%v %q", trunc, p)
	}
}

func TestParsePlanWithNothingToChangeIsNotAnError(t *testing.T) {
	p, sum, err := ParsePlan(`{"summary":"合同整体对我方无不利条款","edits":[],"insertions":[]}`)
	if err != nil || sum == "" || len(p.Edits)+len(p.Insertions) != 0 {
		t.Fatalf("%v %q %+v", err, sum, p)
	}
}
