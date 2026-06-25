package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// makeDocx собирает минимальный .docx (zip + word/document.xml с w:t).
func makeDocx(t *testing.T, text string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	xmlBody := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:body><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:body></w:document>`
	if _, err := f.Write([]byte(xmlBody)); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func multipartUpload(t *testing.T, fieldData []byte, filename string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(fieldData); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	mw.Close()
	return &body, mw.FormDataContentType()
}

func TestIngestDocx(t *testing.T) {
	docx := makeDocx(t, "Гарантия на ноутбук Kivano составляет двадцать четыре месяца.")
	body, contentType := multipartUpload(t, docx, "warranty.docx")

	req := httptest.NewRequest(http.MethodPost, "/v1/ingest/docx", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ingest docx failed: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, "/v1/search", `{"query":"гарантия на ноутбук","top_k":3}`)
	if rec.Code != http.StatusOK || !strings.Contains(strings.ToLower(rec.Body.String()), "двадцать четыре") {
		t.Fatalf("docx search failed: %s", rec.Body.String())
	}
}

func TestExtractDocxText(t *testing.T) {
	docx := makeDocx(t, "Привет мир")
	got, err := extractDocxText(docx)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if got != "Привет мир" {
		t.Fatalf("unexpected docx text: %q", got)
	}
}

func TestIngestWeb(t *testing.T) {
	html := `<html><head><title>Условия оплаты</title></head>
		<body><script>var x=1;</script><style>.a{}</style>
		<h1>Оплата</h1><p>Оплата заказа возможна картой Visa или наличными курьеру.</p></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	payload, _ := json.Marshal(map[string]any{"urls": []string{srv.URL}})
	rec := do(http.MethodPost, "/v1/ingest/web", string(payload))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ingested":1`) {
		t.Fatalf("web ingest failed: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, "/v1/search", `{"query":"как оплатить заказ картой","top_k":3}`)
	low := strings.ToLower(rec.Body.String())
	if rec.Code != http.StatusOK || !strings.Contains(low, "оплата") {
		t.Fatalf("web search failed: %s", rec.Body.String())
	}
	// Скрипты/стили должны быть вырезаны.
	if strings.Contains(low, "var x") {
		t.Fatalf("script not stripped: %s", rec.Body.String())
	}
}

func TestExtractPDFTextUncompressed(t *testing.T) {
	// Минимальный PDF-подобный content stream с операторами Tj/TJ (без сжатия).
	pdf := []byte("%PDF-1.4\n" +
		"4 0 obj\n<< /Length 60 >>\nstream\n" +
		"BT /F1 12 Tf 72 720 Td (Привет Kivano) Tj 0 -14 Td [(База )-5(знаний)] TJ ET\n" +
		"endstream\nendobj\n%%EOF")
	got, err := extractPDFText(pdf)
	if err != nil {
		t.Fatalf("extract pdf: %v", err)
	}
	if !strings.Contains(got, "Kivano") || !strings.Contains(got, "знаний") {
		t.Fatalf("unexpected pdf text: %q", got)
	}
}

func TestIngestPDFNoText(t *testing.T) {
	// Файл без извлекаемого текста → 422.
	body, contentType := multipartUpload(t, []byte("%PDF-1.4 garbage without text operators"), "scan.pdf")
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest/pdf", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	router().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for textless pdf, got %d: %s", rec.Code, rec.Body.String())
	}
}
