package server

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
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

func createTestPDF(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := api.Create(nil, strings.NewReader(testPDFJSON), &buf, nil); err != nil {
		t.Fatalf("failed to create test PDF: %v", err)
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
