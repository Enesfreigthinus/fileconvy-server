package server

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pdfcpu/pdfcpu/pkg/api"
)

const mergedPDFName = "merged.pdf"

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

	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", `attachment; filename="`+mergedPDFName+`"`)
	c.File(outputPath)
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
