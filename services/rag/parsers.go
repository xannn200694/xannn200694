// Парсеры источников (решение D5): веб-сайт, Word (.docx), PDF.
//
// Все парсеры работают на стандартной библиотеке. PDF-извлечение — best-effort
// (см. ограничения в README): поддерживаются текстовые операторы Tj/TJ и потоки
// FlateDecode; сканы/CID-шрифты без ToUnicode не извлекаются.
package main

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// --- Веб-сайт ---------------------------------------------------------------

type webIngestRequest struct {
	URLs []string `json:"urls"`
}

var (
	scriptRe = regexp.MustCompile(`(?is)<script[^>]*>.*?</\s*script\s*>`)
	styleRe  = regexp.MustCompile(`(?is)<style[^>]*>.*?</\s*style\s*>`)
	tagRe    = regexp.MustCompile(`(?s)<[^>]+>`)
	titleRe  = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	entityRe = regexp.MustCompile(`&#(\d+);`)
)

// stripHTML вырезает скрипты/стили/теги и возвращает (title, plainText).
func stripHTML(html string) (string, string) {
	title := ""
	if m := titleRe.FindStringSubmatch(html); m != nil {
		title = unescapeHTML(strings.TrimSpace(tagRe.ReplaceAllString(m[1], " ")))
	}
	body := scriptRe.ReplaceAllString(html, " ")
	body = styleRe.ReplaceAllString(body, " ")
	body = tagRe.ReplaceAllString(body, " ")
	body = unescapeHTML(body)
	body = strings.TrimSpace(wsRe.ReplaceAllString(body, " "))
	return title, body
}

func unescapeHTML(s string) string {
	r := strings.NewReplacer(
		"&nbsp;", " ",
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
		"&apos;", "'",
	)
	s = r.Replace(s)
	s = entityRe.ReplaceAllStringFunc(s, func(m string) string {
		var code int
		if _, err := fmt.Sscanf(m, "&#%d;", &code); err == nil {
			return string(rune(code))
		}
		return m
	})
	return s
}

func handleIngestWeb(w http.ResponseWriter, r *http.Request) {
	var req webIngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid json"})
		return
	}
	if len(req.URLs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "urls is required"})
		return
	}
	client := &http.Client{Timeout: 15 * time.Second}
	ingested, chunks := 0, 0
	var failed []string
	for _, u := range req.URLs {
		resp, err := client.Get(u)
		if err != nil {
			failed = append(failed, u)
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			failed = append(failed, u)
			continue
		}
		title, text := stripHTML(string(data))
		if strings.TrimSpace(text) == "" {
			failed = append(failed, u)
			continue
		}
		if title == "" {
			title = u
		}
		doc := Document{
			DocID:    "web:" + shortHash(u),
			Title:    title,
			Text:     text,
			Source:   u,
			Metadata: map[string]any{"source_type": "web", "url": u},
		}
		n, err := ingestDocument(doc)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"detail": "ingest failed: " + err.Error()})
			return
		}
		ingested++
		chunks += n
	}
	if ingested == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"detail": "no pages ingested", "failed": failed,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ingested": ingested, "chunks": chunks, "failed": failed,
	})
}

// --- Word (.docx) -----------------------------------------------------------

// extractDocxText распаковывает .docx (zip) и собирает текст из элементов w:t.
func extractDocxText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("not a valid .docx (zip): %w", err)
	}
	var docFile *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			docFile = f
			break
		}
	}
	if docFile == nil {
		return "", fmt.Errorf("word/document.xml not found")
	}
	rc, err := docFile.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	dec := xml.NewDecoder(rc)
	var sb strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" {
				var content string
				if err := dec.DecodeElement(&content, &t); err != nil {
					return "", err
				}
				sb.WriteString(content)
			}
		case xml.EndElement:
			// Конец абзаца/строки — разделитель, чтобы слова не слипались.
			if t.Name.Local == "p" || t.Name.Local == "br" || t.Name.Local == "tab" {
				sb.WriteString(" ")
			}
		}
	}
	return strings.TrimSpace(sb.String()), nil
}

func handleIngestDocx(w http.ResponseWriter, r *http.Request) {
	data, filename, err := readUpload(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": err.Error()})
		return
	}
	text, err := extractDocxText(data)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"detail": "docx parse failed: " + err.Error()})
		return
	}
	if strings.TrimSpace(text) == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"detail": "docx contains no text"})
		return
	}
	title := r.FormValue("title")
	if title == "" {
		title = filename
	}
	doc := Document{
		DocID:    "docx:" + shortHash(filename+":"+text),
		Title:    title,
		Text:     text,
		Source:   filename,
		Metadata: map[string]any{"source_type": "docx", "filename": filename},
	}
	n, err := ingestDocument(doc)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"detail": "ingest failed: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ingested": 1, "chunks": n, "doc_id": doc.DocID})
}

// --- PDF (best-effort) ------------------------------------------------------

var (
	streamRe = regexp.MustCompile(`(?s)stream\r?\n(.*?)\r?\nendstream`)
	tjRe     = regexp.MustCompile(`(?s)\((?:[^()\\]|\\.)*\)\s*Tj`)
	tjArrRe  = regexp.MustCompile(`(?s)\[(.*?)\]\s*TJ`)
	pdfStrRe = regexp.MustCompile(`(?s)\((?:[^()\\]|\\.)*\)`)
)

// extractPDFText — best-effort извлечение текста из PDF: распаковка FlateDecode-потоков
// (zlib) и парсинг текстовых операторов Tj/TJ. Возвращает ошибку, если текста нет.
func extractPDFText(data []byte) (string, error) {
	var sb strings.Builder
	matches := streamRe.FindAllSubmatch(data, -1)
	chunks := [][]byte{}
	if len(matches) > 0 {
		for _, m := range matches {
			raw := m[1]
			if dec, err := flateDecode(raw); err == nil {
				chunks = append(chunks, dec)
			} else {
				chunks = append(chunks, raw)
			}
		}
	} else {
		// Нет потоков — пробуем разобрать содержимое целиком (uncompressed PDF).
		chunks = append(chunks, data)
	}
	for _, content := range chunks {
		extractTextOperators(content, &sb)
	}
	text := strings.TrimSpace(wsRe.ReplaceAllString(sb.String(), " "))
	if text == "" {
		return "", fmt.Errorf("no extractable text (scanned or unsupported encoding)")
	}
	return text, nil
}

func flateDecode(b []byte) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

func extractTextOperators(content []byte, sb *strings.Builder) {
	for _, m := range tjRe.FindAll(content, -1) {
		if s := pdfStrRe.Find(m); s != nil {
			sb.WriteString(decodePDFString(s))
			sb.WriteString(" ")
		}
	}
	for _, m := range tjArrRe.FindAllSubmatch(content, -1) {
		for _, s := range pdfStrRe.FindAll(m[1], -1) {
			sb.WriteString(decodePDFString(s))
		}
		sb.WriteString(" ")
	}
}

// decodePDFString снимает скобки и разворачивает escape-последовательности PDF.
func decodePDFString(s []byte) string {
	if len(s) >= 2 && s[0] == '(' && s[len(s)-1] == ')' {
		s = s[1 : len(s)-1]
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case '(', ')', '\\':
				sb.WriteByte(s[i])
			default:
				sb.WriteByte(s[i])
			}
			continue
		}
		sb.WriteByte(c)
	}
	return sb.String()
}

func handleIngestPDF(w http.ResponseWriter, r *http.Request) {
	data, filename, err := readUpload(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": err.Error()})
		return
	}
	text, err := extractPDFText(data)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"detail": "pdf text extraction failed (best-effort, stdlib): " + err.Error(),
		})
		return
	}
	title := r.FormValue("title")
	if title == "" {
		title = filename
	}
	doc := Document{
		DocID:    "pdf:" + shortHash(filename+":"+text),
		Title:    title,
		Text:     text,
		Source:   filename,
		Metadata: map[string]any{"source_type": "pdf", "filename": filename},
	}
	n, err := ingestDocument(doc)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"detail": "ingest failed: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ingested": 1, "chunks": n, "doc_id": doc.DocID})
}

// --- Общие помощники --------------------------------------------------------

// readUpload читает multipart-файл из поля "file".
func readUpload(r *http.Request) ([]byte, string, error) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return nil, "", fmt.Errorf("invalid multipart form: %w", err)
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, "", fmt.Errorf("missing form field 'file'")
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, "", err
	}
	name := "upload"
	if header != nil && header.Filename != "" {
		name = header.Filename
	}
	return data, name, nil
}

func shortHash(s string) string {
	sum := sha1.Sum([]byte(s))
	return fmt.Sprintf("%x", sum[:8])
}
