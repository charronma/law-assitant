// Package export renders assistant output as Word documents.
package export

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
)

// Disclaimer is appended to every exported document.
const Disclaimer = "本文件由 AI 生成，仅供参考，不构成法律意见。重要事项请咨询执业律师。"

// MarkdownToDocx converts the Markdown subset LLMs actually produce (headings,
// paragraphs, bullet/numbered lists, block quotes, fenced code, pipe tables,
// **bold**, *italic*, `code`) into a .docx. title may be empty.
func MarkdownToDocx(title, markdown string) ([]byte, error) {
	var body strings.Builder
	if strings.TrimSpace(title) != "" {
		body.WriteString(para("Title", "", runs(title, false)))
	}
	renderBlocks(&body, markdown)
	body.WriteString(para("", `<w:spacing w:before="480"/>`, run(Disclaimer, runFmt{color: "808080", size: 18})))

	doc := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		body.String() +
		`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1304" w:bottom="1440" w:left="1304" w:header="851" w:footer="992" w:gutter="0"/></w:sectPr>` +
		`</w:body></w:document>`

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	parts := []struct{ name, data string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", rootRels},
		{"word/document.xml", doc},
		{"word/_rels/document.xml.rels", docRels},
		{"word/styles.xml", styles},
	}
	for _, p := range parts {
		w, err := zw.Create(p.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(p.data)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// FirstHeading returns the text of the first Markdown heading, or "".
func FirstHeading(markdown string) string {
	for _, l := range strings.Split(markdown, "\n") {
		if m := headingRe.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			return stripInline(m[2])
		}
	}
	return ""
}

var (
	headingRe = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*$`)
	bulletRe  = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	orderedRe = regexp.MustCompile(`^(\s*)(\d+)[.)]\s+(.*)$`)
	ruleRe    = regexp.MustCompile(`^\s*(-\s*){3,}$|^\s*(\*\s*){3,}$|^\s*(_\s*){3,}$`)
	tableSep  = regexp.MustCompile(`^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$`)
)

func renderBlocks(b *strings.Builder, md string) {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(md, "\r\n", "\n"), "\r", "\n"), "\n")
	var paragraph []string
	flush := func() {
		if len(paragraph) == 0 {
			return
		}
		b.WriteString(para("", "", runsLines(paragraph)))
		paragraph = nil
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "":
			flush()

		case strings.HasPrefix(trimmed, "```"):
			flush()
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
				code = append(code, lines[i])
			}
			for _, c := range code {
				b.WriteString(para("", `<w:ind w:left="360"/>`, run(c, runFmt{mono: true})))
			}

		case headingRe.MatchString(trimmed):
			flush()
			m := headingRe.FindStringSubmatch(trimmed)
			level := len(m[1])
			if level > 3 {
				level = 3
			}
			b.WriteString(para(fmt.Sprintf("Heading%d", level), "", runs(m[2], false)))

		case ruleRe.MatchString(trimmed):
			flush()
			b.WriteString(para("", `<w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="BBBBBB"/></w:pBdr>`, ""))

		case strings.HasPrefix(trimmed, "|") && i+1 < len(lines) && tableSep.MatchString(lines[i+1]):
			flush()
			var rows [][]string
			rows = append(rows, splitRow(trimmed))
			for i += 2; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
				rows = append(rows, splitRow(strings.TrimSpace(lines[i])))
			}
			i--
			b.WriteString(table(rows))

		case bulletRe.MatchString(line):
			flush()
			m := bulletRe.FindStringSubmatch(line)
			b.WriteString(listItem(len(m[1]), "• ", m[2]))

		case orderedRe.MatchString(line):
			flush()
			m := orderedRe.FindStringSubmatch(line)
			b.WriteString(listItem(len(m[1]), m[2]+". ", m[3]))

		case strings.HasPrefix(trimmed, ">"):
			flush()
			text := strings.TrimSpace(strings.TrimLeft(trimmed, "> "))
			b.WriteString(para("", `<w:pBdr><w:left w:val="single" w:sz="12" w:space="8" w:color="BBBBBB"/></w:pBdr><w:ind w:left="480"/>`, runs(text, false)))

		default:
			paragraph = append(paragraph, trimmed)
		}
	}
	flush()
}

func listItem(indentSpaces int, marker, text string) string {
	level := indentSpaces / 2
	if level > 4 {
		level = 4
	}
	left := 420 + level*420
	ppr := fmt.Sprintf(`<w:ind w:left="%d" w:hanging="300"/>`, left)
	return para("", ppr, run(marker, runFmt{})+runs(text, false))
}

func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	cells := strings.Split(line, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

func table(rows [][]string) string {
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	var b strings.Builder
	b.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="5000" w:type="pct"/><w:tblBorders>`)
	for _, side := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
		fmt.Fprintf(&b, `<w:%s w:val="single" w:sz="4" w:space="0" w:color="999999"/>`, side)
	}
	b.WriteString(`</w:tblBorders><w:tblCellMar><w:left w:w="100" w:type="dxa"/><w:right w:w="100" w:type="dxa"/></w:tblCellMar></w:tblPr><w:tblGrid>`)
	for i := 0; i < cols; i++ {
		b.WriteString(`<w:gridCol w:w="2000"/>`)
	}
	b.WriteString(`</w:tblGrid>`)
	for ri, r := range rows {
		b.WriteString(`<w:tr>`)
		for ci := 0; ci < cols; ci++ {
			cell := ""
			if ci < len(r) {
				cell = r[ci]
			}
			shade := ""
			if ri == 0 {
				shade = `<w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/>`
			}
			fmt.Fprintf(&b, `<w:tc><w:tcPr>%s</w:tcPr>%s</w:tc>`, shade, para("", "", runs(cell, ri == 0)))
		}
		b.WriteString(`</w:tr>`)
	}
	b.WriteString(`</w:tbl>`)
	// A paragraph must follow a table before the next table or section end.
	b.WriteString(para("", "", ""))
	return b.String()
}

func para(style, extraPPr, content string) string {
	var ppr string
	if style != "" {
		ppr += fmt.Sprintf(`<w:pStyle w:val="%s"/>`, style)
	}
	ppr += extraPPr
	if ppr != "" {
		ppr = "<w:pPr>" + ppr + "</w:pPr>"
	}
	return "<w:p>" + ppr + content + "</w:p>"
}

type runFmt struct {
	bold, italic, mono bool
	color              string
	size               int // half-points; 0 = style default
}

func run(text string, f runFmt) string {
	var rpr string
	if f.mono {
		rpr += `<w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:eastAsia="SimSun"/>`
	}
	if f.bold {
		rpr += `<w:b/>`
	}
	if f.italic {
		rpr += `<w:i/>`
	}
	if f.color != "" {
		rpr += fmt.Sprintf(`<w:color w:val="%s"/>`, f.color)
	}
	if f.size > 0 {
		rpr += fmt.Sprintf(`<w:sz w:val="%d"/>`, f.size)
	}
	if rpr != "" {
		rpr = "<w:rPr>" + rpr + "</w:rPr>"
	}
	return `<w:r>` + rpr + `<w:t xml:space="preserve">` + escape(text) + `</w:t></w:r>`
}

func runsLines(lines []string) string {
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteString(`<w:r><w:br/></w:r>`)
		}
		b.WriteString(runs(l, false))
	}
	return b.String()
}

var inlineRe = regexp.MustCompile("(\\*\\*[^*]+\\*\\*|__[^_]+__|\\*[^*\\s][^*]*\\*|`[^`]+`|\\[[^\\]]+\\]\\([^)]+\\))")

// runs renders inline Markdown. forceBold is used for table headers.
func runs(text string, forceBold bool) string {
	var b strings.Builder
	last := 0
	for _, loc := range inlineRe.FindAllStringIndex(text, -1) {
		if loc[0] > last {
			b.WriteString(run(text[last:loc[0]], runFmt{bold: forceBold}))
		}
		tok := text[loc[0]:loc[1]]
		switch {
		case strings.HasPrefix(tok, "**"), strings.HasPrefix(tok, "__"):
			b.WriteString(run(tok[2:len(tok)-2], runFmt{bold: true}))
		case strings.HasPrefix(tok, "`"):
			b.WriteString(run(tok[1:len(tok)-1], runFmt{mono: true, bold: forceBold}))
		case strings.HasPrefix(tok, "["):
			end := strings.Index(tok, "](")
			b.WriteString(run(tok[1:end]+" ("+tok[end+2:len(tok)-1]+")", runFmt{bold: forceBold}))
		default:
			b.WriteString(run(tok[1:len(tok)-1], runFmt{italic: true, bold: forceBold}))
		}
		last = loc[1]
	}
	if last < len(text) {
		b.WriteString(run(text[last:], runFmt{bold: forceBold}))
	}
	return b.String()
}

func stripInline(s string) string {
	return inlineRe.ReplaceAllStringFunc(s, func(tok string) string {
		switch {
		case strings.HasPrefix(tok, "**"), strings.HasPrefix(tok, "__"):
			return tok[2 : len(tok)-2]
		case strings.HasPrefix(tok, "["):
			return tok[1:strings.Index(tok, "](")]
		default:
			return tok[1 : len(tok)-1]
		}
	})
}

// escape XML-escapes text and drops characters XML 1.0 forbids.
func escape(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || r >= 0x20 && r != 0xFFFE && r != 0xFFFF && (r < 0xD800 || r > 0xDFFF) {
			return r
		}
		return -1
	}, s)
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

const contentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
	`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
	`</Types>`

const rootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
	`</Relationships>`

const docRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
	`</Relationships>`

// Body text: 12pt SimSun (宋体) for CJK, Times New Roman for Latin; headings in SimHei (黑体).
const styles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
	`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Times New Roman" w:hAnsi="Times New Roman" w:eastAsia="SimSun" w:cs="Times New Roman"/><w:sz w:val="24"/><w:szCs w:val="24"/><w:lang w:val="en-US" w:eastAsia="zh-CN"/></w:rPr></w:rPrDefault>` +
	`<w:pPrDefault><w:pPr><w:spacing w:after="120" w:line="360" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>` +
	`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:before="240" w:after="360"/><w:jc w:val="center"/></w:pPr><w:rPr><w:rFonts w:eastAsia="SimHei"/><w:b/><w:sz w:val="36"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="360" w:after="160"/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:rFonts w:eastAsia="SimHei"/><w:b/><w:sz w:val="32"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="280" w:after="120"/><w:outlineLvl w:val="1"/></w:pPr><w:rPr><w:rFonts w:eastAsia="SimHei"/><w:b/><w:sz w:val="28"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading3"><w:name w:val="heading 3"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="200" w:after="100"/><w:outlineLvl w:val="2"/></w:pPr><w:rPr><w:rFonts w:eastAsia="SimHei"/><w:b/><w:sz w:val="26"/></w:rPr></w:style>` +
	`</w:styles>`
