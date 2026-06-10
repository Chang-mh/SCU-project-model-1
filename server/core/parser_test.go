package core

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestExtractTextFromTXT(t *testing.T) {
	text, ext, err := ExtractText("customer.txt", []byte("客户名称：四川示例科技有限公司\n联系人：张三"))
	if err != nil {
		t.Fatalf("ExtractText() error = %v", err)
	}
	if ext != "txt" {
		t.Fatalf("ExtractText() ext = %q, want txt", ext)
	}
	if !strings.Contains(text, "客户名称：四川示例科技有限公司") || !strings.Contains(text, "联系人：张三") {
		t.Fatalf("ExtractText() text = %q", text)
	}
}

func TestExtractTextFromXLSX(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetCellValue("Sheet1", "A1", "客户名称"); err != nil {
		t.Fatalf("SetCellValue() error = %v", err)
	}
	if err := f.SetCellValue("Sheet1", "B1", "报价50万元"); err != nil {
		t.Fatalf("SetCellValue() error = %v", err)
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatalf("WriteToBuffer() error = %v", err)
	}

	text, ext, err := ExtractText("quote.xlsx", buf.Bytes())
	if err != nil {
		t.Fatalf("ExtractText() error = %v", err)
	}
	if ext != "xlsx" {
		t.Fatalf("ExtractText() ext = %q, want xlsx", ext)
	}
	if !strings.Contains(text, "客户名称") || !strings.Contains(text, "报价50万元") {
		t.Fatalf("ExtractText() text = %q", text)
	}
}

func TestExtractTextFromDOCX(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_, err = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>客户资料</w:t></w:r></w:p><w:p><w:r><w:t>联系人张三</w:t></w:r></w:p></w:body></w:document>`))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	text, ext, err := ExtractText("customer.docx", buf.Bytes())
	if err != nil {
		t.Fatalf("ExtractText() error = %v", err)
	}
	if ext != "docx" {
		t.Fatalf("ExtractText() ext = %q, want docx", ext)
	}
	if !strings.Contains(text, "客户资料") || !strings.Contains(text, "联系人张三") {
		t.Fatalf("ExtractText() text = %q", text)
	}
}
