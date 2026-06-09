package core

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

func ExtractText(fileName string, data []byte) (string, string, error) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(fileName), "."))
	switch ext {
	case "txt", "csv", "json", "xml", "md", "py", "java", "go", "sql", "conf", "config", "yaml", "yml", "ini", "log":
		return cleanText(bytesToText(data)), ext, nil
	case "xlsx":
		text, err := extractXLSX(data)
		return cleanText(text), ext, err
	case "docx":
		text, err := extractDOCX(data)
		return cleanText(text), ext, err
	case "pdf":
		return cleanText(extractPDFLikeText(data)), ext, nil
	default:
		return cleanText(bytesToText(data)), ext, nil
	}
}

func bytesToText(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	return strings.Map(func(r rune) rune {
		if r == utf8.RuneError {
			return ' '
		}
		return r
	}, string(data))
}

func cleanText(text string) string {
	text = strings.ReplaceAll(text, "\x00", " ")
	space := regexp.MustCompile(`\s+`)
	return strings.TrimSpace(space.ReplaceAllString(text, " "))
}

func extractXLSX(data []byte) (string, error) {
	reader := bytes.NewReader(data)
	f, err := excelize.OpenReader(reader)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var parts []string
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil {
			continue
		}
		for _, row := range rows {
			parts = append(parts, strings.Join(row, " "))
		}
	}
	return strings.Join(parts, "\n"), nil
}

func extractDOCX(data []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	for _, file := range reader.File {
		if file.Name != "word/document.xml" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		content, err := io.ReadAll(rc)
		if err != nil {
			return "", err
		}
		return extractXMLText(content), nil
	}
	return "", fmt.Errorf("docx document xml not found")
}

func extractXMLText(data []byte) string {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var parts []string
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		if charData, ok := token.(xml.CharData); ok {
			value := strings.TrimSpace(string(charData))
			if value != "" {
				parts = append(parts, value)
			}
		}
	}
	return strings.Join(parts, " ")
}

func extractPDFLikeText(data []byte) string {
	text := bytesToText(data)
	re := regexp.MustCompile(`[\x20-\x7e\p{Han}]{3,}`)
	return strings.Join(re.FindAllString(text, -1), " ")
}
