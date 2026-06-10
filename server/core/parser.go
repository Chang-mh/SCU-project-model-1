package core

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

const (
	EnvPaddleOCRAPIURL     = "PADDLEOCR_API_URL"
	EnvPaddleOCRAPIKey     = "PADDLEOCR_API_KEY"
	DefaultPaddleOCRAPIURL = "xxx"
	DefaultPaddleOCRAPIKey = "xxx"
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
		text, err := extractPDFWithPaddleOCR(fileName, data)
		if err != nil {
			fallback := extractPDFLikeText(data)
			if fallback != "" {
				return cleanText(fallback), ext, fmt.Errorf("paddleocr pdf extraction failed, used fallback: %w", err)
			}
			return "", ext, err
		}
		return cleanText(text), ext, nil
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

func extractPDFWithPaddleOCR(fileName string, data []byte) (string, error) {
	apiURL := strings.TrimSpace(os.Getenv(EnvPaddleOCRAPIURL))
	if apiURL == "" {
		apiURL = DefaultPaddleOCRAPIURL
	}
	apiKey := strings.TrimSpace(os.Getenv(EnvPaddleOCRAPIKey))
	if apiKey == "" {
		apiKey = DefaultPaddleOCRAPIKey
	}
	if apiURL == "" || apiURL == "xxx" {
		return "", errors.New("paddleocr api url not configured")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := writer.WriteField("type", "pdf"); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, apiURL, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if apiKey != "" && apiKey != "xxx" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("paddleocr request failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}
	text, err := parsePaddleOCRText(respBody)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("paddleocr response has empty text")
	}
	return text, nil
}

func parsePaddleOCRText(data []byte) (string, error) {
	var resp struct {
		Text   string `json:"text"`
		Result any    `json:"result"`
		Data   any    `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", err
	}
	if resp.Text != "" {
		return resp.Text, nil
	}
	for _, value := range []any{resp.Result, resp.Data} {
		if text := extractTextFromOCRValue(value); text != "" {
			return text, nil
		}
	}
	return "", errors.New("paddleocr response text field not found")
}

func extractTextFromOCRValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := extractTextFromOCRValue(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		for _, key := range []string{"text", "content", "recognized_text", "ocr_text"} {
			if text := extractTextFromOCRValue(v[key]); text != "" {
				return text
			}
		}
		if lines := extractTextFromOCRValue(v["lines"]); lines != "" {
			return lines
		}
	}
	return ""
}

func extractPDFLikeText(data []byte) string {
	text := bytesToText(data)
	re := regexp.MustCompile(`[\x20-\x7e\p{Han}]{3,}`)
	return strings.Join(re.FindAllString(text, -1), " ")
}
