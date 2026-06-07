package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/pdfcpu/pdfcpu/pkg/api"
)

const mergedPDFName = "merged.pdf"
const splitPDFName = "split.pdf"
const compressedPDFName = "compressed.pdf"
const convertedPDFName = "converted.pdf"
const libreOfficeConvertTimeout = 2 * time.Minute

var runLibreOfficeConvert = func(ctx context.Context, inputPath, outputDir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "libreoffice", "--headless", "--convert-to", "pdf", "--outdir", outputDir, inputPath)
	return cmd.CombinedOutput()
}

func mergePDFs(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected multipart form-data with PDF files"})
		return
	}

	files := uploadedFiles(form)
	if len(files) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "please upload at least two PDF files"})
		return
	}

	tempDir, err := os.MkdirTemp("", "fileconvy-pdf-merge-*")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to prepare temporary storage"})
		return
	}
	defer os.RemoveAll(tempDir)

	inputPaths := make([]string, 0, len(files))
	for i, file := range files {
		inputPath := filepath.Join(tempDir, fmt.Sprintf("input-%03d.pdf", i+1))
		if err := savePDFUpload(file, inputPath); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		inputPaths = append(inputPaths, inputPath)
	}

	outputPath := filepath.Join(tempDir, mergedPDFName)
	if err := api.MergeCreateFile(inputPaths, outputPath, false, nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to merge PDF files"})
		return
	}

	sendPDFFile(c, outputPath, mergedPDFName)
}

func splitPDF(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected multipart form-data with a PDF file field named file"})
		return
	}

	selectedPages, err := parseSelectedPages(c.PostForm("pages"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tempDir, err := os.MkdirTemp("", "fileconvy-pdf-split-*")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to prepare temporary storage"})
		return
	}
	defer os.RemoveAll(tempDir)

	inputPath := filepath.Join(tempDir, "input.pdf")
	if err := savePDFUpload(file, inputPath); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	outputPath := filepath.Join(tempDir, splitPDFName)
	if err := api.CollectFile(inputPath, outputPath, selectedPages, nil); err != nil {
		status := http.StatusInternalServerError
		message := "failed to split PDF file"
		if strings.Contains(err.Error(), "no page selected") {
			status = http.StatusBadRequest
			message = "selected pages do not exist in the uploaded PDF"
		}
		c.JSON(status, gin.H{"error": message})
		return
	}

	sendPDFFile(c, outputPath, splitPDFName)
}

func compressPDF(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected multipart form-data with a PDF file field named file"})
		return
	}

	tempDir, err := os.MkdirTemp("", "fileconvy-pdf-compress-*")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to prepare temporary storage"})
		return
	}
	defer os.RemoveAll(tempDir)

	inputPath := filepath.Join(tempDir, "input.pdf")
	if err := savePDFUpload(file, inputPath); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	outputPath := filepath.Join(tempDir, compressedPDFName)
	if err := api.OptimizeFile(inputPath, outputPath, nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to compress PDF file"})
		return
	}

	sendPDFFile(c, outputPath, compressedPDFName)
}

func convertToPDF(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected multipart form-data with a file field named file"})
		return
	}

	fileKind, err := supportedConversionKind(file.Filename)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tempDir, err := os.MkdirTemp("", "fileconvy-pdf-convert-*")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to prepare temporary storage"})
		return
	}
	defer os.RemoveAll(tempDir)

	inputPath := filepath.Join(tempDir, "input"+strings.ToLower(filepath.Ext(file.Filename)))
	if err := saveUpload(file, inputPath); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	outputPath := filepath.Join(tempDir, convertedPDFName)
	switch fileKind {
	case "image":
		if err := api.ImportImagesFile([]string{inputPath}, outputPath, nil, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to convert image to PDF"})
			return
		}
	case "office":
		officeOutputPath, err := convertOfficeDocumentToPDF(c.Request.Context(), inputPath, tempDir)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		outputPath = officeOutputPath
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported file type"})
		return
	}

	sendPDFFile(c, outputPath, convertedPDFName)
}

func uploadedFiles(form *multipart.Form) []*multipart.FileHeader {
	if files := form.File["files"]; len(files) > 0 {
		return files
	}

	fields := make([]string, 0, len(form.File))
	for field := range form.File {
		fields = append(fields, field)
	}
	sort.Strings(fields)

	files := make([]*multipart.FileHeader, 0)
	for _, field := range fields {
		files = append(files, form.File[field]...)
	}

	return files
}

func parseSelectedPages(rawPages string) ([]string, error) {
	pagesExpression := withoutWhitespace(rawPages)
	if pagesExpression == "" {
		return nil, errors.New("pages field is required")
	}

	selectedPages, err := api.ParsePageSelection(pagesExpression)
	if err != nil {
		return nil, fmt.Errorf("invalid pages format: use values like 1,3,5 or 2-5")
	}

	return selectedPages, nil
}

func withoutWhitespace(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}

	return b.String()
}

func supportedConversionKind(fileName string) (string, error) {
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".png", ".jpg", ".jpeg":
		return "image", nil
	case ".docx", ".pptx", ".xlsx":
		return "office", nil
	default:
		return "", fmt.Errorf("%s is not a supported conversion file type", fileName)
	}
}

func convertOfficeDocumentToPDF(parent context.Context, inputPath, outputDir string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, libreOfficeConvertTimeout)
	defer cancel()

	output, err := runLibreOfficeConvert(ctx, inputPath, outputDir)
	if ctx.Err() == context.DeadlineExceeded {
		return "", errors.New("timed out while converting office document to PDF")
	}
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", errors.New("LibreOffice is not available on this server")
		}
		return "", fmt.Errorf("failed to convert office document to PDF: %s", strings.TrimSpace(string(output)))
	}

	outputPath := strings.TrimSuffix(inputPath, filepath.Ext(inputPath)) + ".pdf"
	if _, err := os.Stat(outputPath); err != nil {
		return "", errors.New("failed to find converted PDF output")
	}

	return outputPath, nil
}

func sendPDFFile(c *gin.Context, filePath, fileName string) {
	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", `attachment; filename="`+fileName+`"`)
	c.File(filePath)
}

func saveUpload(fileHeader *multipart.FileHeader, destination string) error {
	source, err := fileHeader.Open()
	if err != nil {
		return fmt.Errorf("failed to read %s", fileHeader.Filename)
	}
	defer source.Close()

	target, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("failed to save %s", fileHeader.Filename)
	}
	defer target.Close()

	if _, err := io.Copy(target, source); err != nil {
		return fmt.Errorf("failed to save %s", fileHeader.Filename)
	}

	return nil
}

func savePDFUpload(fileHeader *multipart.FileHeader, destination string) error {
	if !strings.EqualFold(filepath.Ext(fileHeader.Filename), ".pdf") {
		return fmt.Errorf("%s is not a PDF file", fileHeader.Filename)
	}

	source, err := fileHeader.Open()
	if err != nil {
		return fmt.Errorf("failed to read %s", fileHeader.Filename)
	}
	defer source.Close()

	header := make([]byte, 5)
	n, err := io.ReadFull(source, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("failed to read %s", fileHeader.Filename)
	}
	if n != len(header) || string(header) != "%PDF-" {
		return fmt.Errorf("%s is not a valid PDF file", fileHeader.Filename)
	}

	target, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("failed to save %s", fileHeader.Filename)
	}
	defer target.Close()

	if _, err := target.Write(header); err != nil {
		return fmt.Errorf("failed to save %s", fileHeader.Filename)
	}
	if _, err := io.Copy(target, source); err != nil {
		return fmt.Errorf("failed to save %s", fileHeader.Filename)
	}

	return nil
}
