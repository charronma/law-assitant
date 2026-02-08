package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DocumentParser parses document files and extracts text content
type DocumentParser struct{}

// NewDocumentParser creates a new DocumentParser
func NewDocumentParser() *DocumentParser {
	return &DocumentParser{}
}

// Parse extracts text content from a document file
func (p *DocumentParser) Parse(filePath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))

	switch ext {
	case ".txt", ".md":
		return p.parseText(filePath)
	case ".pdf":
		return p.parsePDF(filePath)
	case ".docx":
		return p.parseDocx(filePath)
	default:
		return "", fmt.Errorf("unsupported file format: %s", ext)
	}
}

// parseText reads plain text files
func (p *DocumentParser) parseText(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read text file: %w", err)
	}
	return string(data), nil
}

// parsePDF extracts text from PDF files
// Uses a simplified approach - reads raw content and extracts readable text
func (p *DocumentParser) parsePDF(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read PDF file: %w", err)
	}

	// Simple PDF text extraction
	// For production use, consider using a proper PDF library like pdfcpu or unipdf
	content := string(data)
	var textParts []string

	// Extract text between BT (Begin Text) and ET (End Text) markers
	inText := false
	var currentText strings.Builder

	for i := 0; i < len(content)-1; i++ {
		if content[i] == 'B' && content[i+1] == 'T' {
			inText = true
			continue
		}
		if content[i] == 'E' && content[i+1] == 'T' {
			if currentText.Len() > 0 {
				textParts = append(textParts, currentText.String())
				currentText.Reset()
			}
			inText = false
			continue
		}
		if inText {
			// Extract text from Tj and TJ operators
			if content[i] == '(' {
				j := i + 1
				for j < len(content) && content[j] != ')' {
					currentText.WriteByte(content[j])
					j++
				}
				currentText.WriteByte(' ')
				i = j
			}
		}
	}

	result := strings.Join(textParts, "\n")
	if strings.TrimSpace(result) == "" {
		return "[PDF文件内容无法提取纯文本，建议使用文字版PDF或Word格式上传]", nil
	}

	return result, nil
}

// parseDocx extracts text from Word documents
// Uses a simplified approach - reads the XML content from the docx zip file
func (p *DocumentParser) parseDocx(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read docx file: %w", err)
	}

	// Docx is a zip file, but for simplicity we search for text between XML tags
	// For production use, consider using a proper docx library
	content := string(data)
	var textParts []string

	// Extract text from <w:t> tags
	searchTag := "<w:t"
	closeTag := "</w:t>"

	idx := 0
	for idx < len(content) {
		start := strings.Index(content[idx:], searchTag)
		if start == -1 {
			break
		}
		start += idx

		// Find the end of the opening tag
		tagEnd := strings.Index(content[start:], ">")
		if tagEnd == -1 {
			break
		}
		textStart := start + tagEnd + 1

		// Find closing tag
		end := strings.Index(content[textStart:], closeTag)
		if end == -1 {
			break
		}
		end += textStart

		text := content[textStart:end]
		if strings.TrimSpace(text) != "" {
			textParts = append(textParts, text)
		}

		idx = end + len(closeTag)
	}

	result := strings.Join(textParts, "")
	if strings.TrimSpace(result) == "" {
		return "[Word文件内容无法提取，请确认文件格式正确]", nil
	}

	return result, nil
}

// IsSupportedFormat checks if the file format is supported
func IsSupportedFormat(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".pdf", ".docx", ".doc", ".txt", ".md":
		return true
	default:
		return false
	}
}
