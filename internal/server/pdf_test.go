package server

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

const testPDFJSON = `{
	"paper": "A4P",
	"fonts": {
		"body": {
			"name": "Helvetica",
			"size": 12
		}
	},
	"pages": {
		"1": {
			"content": {
				"text": [
					{
						"value": "Fileconvy compression test",
						"pos": [72, 720],
						"font": {
							"name": "$body"
						}
					}
				]
			}
		}
	}
}`

func TestParseSelectedPages(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{
			name:  "comma separated pages",
			input: "1,3,5",
			want:  []string{"1", "3", "5"},
		},
		{
			name:  "range",
			input: "2-5",
			want:  []string{"2-5"},
		},
		{
			name:  "whitespace around values",
			input: "1, 3, 5",
			want:  []string{"1", "3", "5"},
		},
		{
			name:    "empty pages",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid pages",
			input:   "1,",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSelectedPages(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseSelectedPages(%q) expected error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSelectedPages(%q) unexpected error: %v", tt.input, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseSelectedPages(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCompressPDF(t *testing.T) {
	pdfBytes := createTestPDF(t)
	body, contentType := multipartBody(t, "file", "input.pdf", pdfBytes)

	req := httptest.NewRequest(http.MethodPost, "/api/pdf/compress", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	NewRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/pdf/compress status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/pdf" {
		t.Fatalf("Content-Type = %q, want application/pdf", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="compressed.pdf"` {
		t.Fatalf("Content-Disposition = %q, want compressed PDF attachment", got)
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF-")) {
		t.Fatalf("response body does not look like a PDF")
	}
	if err := api.Validate(bytes.NewReader(rec.Body.Bytes()), nil); err != nil {
		t.Fatalf("compressed PDF response failed validation: %v", err)
	}
}

func TestCompressPDFMissingFile(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/pdf/compress", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	NewRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/pdf/compress status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestConvertImageToPDF(t *testing.T) {
	body, contentType := multipartBody(t, "file", "input.png", createTestPNG(t))

	req := httptest.NewRequest(http.MethodPost, "/api/pdf/convert", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	NewRouter().ServeHTTP(rec, req)

	assertPDFResponse(t, rec, `attachment; filename="converted.pdf"`)
}

func TestConvertOfficeDocumentToPDF(t *testing.T) {
	originalRunLibreOfficeConvert := runLibreOfficeConvert
	t.Cleanup(func() {
		runLibreOfficeConvert = originalRunLibreOfficeConvert
	})

	runLibreOfficeConvert = func(ctx context.Context, inputPath, outputDir string) ([]byte, error) {
		if filepath.Base(inputPath) != "input.docx" {
			t.Fatalf("libreoffice input = %q, want input.docx", inputPath)
		}
		if filepath.Dir(inputPath) != outputDir {
			t.Fatalf("libreoffice output dir = %q, want %q", outputDir, filepath.Dir(inputPath))
		}

		outputPath := strings.TrimSuffix(inputPath, filepath.Ext(inputPath)) + ".pdf"
		if err := os.WriteFile(outputPath, createTestPDF(t), 0600); err != nil {
			t.Fatalf("failed to write fake libreoffice output: %v", err)
		}

		return []byte("convert ok"), nil
	}

	body, contentType := multipartBody(t, "file", "report.docx", []byte("docx content"))

	req := httptest.NewRequest(http.MethodPost, "/api/pdf/convert", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	NewRouter().ServeHTTP(rec, req)

	assertPDFResponse(t, rec, `attachment; filename="converted.pdf"`)
}

func TestConvertUnsupportedFile(t *testing.T) {
	body, contentType := multipartBody(t, "file", "notes.txt", []byte("plain text"))

	req := httptest.NewRequest(http.MethodPost, "/api/pdf/convert", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	NewRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/pdf/convert status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func createTestPDF(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := api.Create(nil, strings.NewReader(testPDFJSON), &buf, nil); err != nil {
		t.Fatalf("failed to create test PDF: %v", err)
	}

	return buf.Bytes()
}

func createTestPNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 20, G: 120, B: 210, A: 255})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("failed to create test PNG: %v", err)
	}

	return buf.Bytes()
}

func multipartBody(t *testing.T, fieldName, fileName string, content []byte) (io.Reader, string) {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile(fieldName, fileName)
	if err != nil {
		t.Fatalf("failed to create multipart file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("failed to write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	return body, writer.FormDataContentType()
}

func assertPDFResponse(t *testing.T, rec *httptest.ResponseRecorder, contentDisposition string) {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/pdf" {
		t.Fatalf("Content-Type = %q, want application/pdf", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != contentDisposition {
		t.Fatalf("Content-Disposition = %q, want %q", got, contentDisposition)
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF-")) {
		t.Fatalf("response body does not look like a PDF")
	}
	if err := api.Validate(bytes.NewReader(rec.Body.Bytes()), nil); err != nil {
		t.Fatalf("PDF response failed validation: %v", err)
	}
}
