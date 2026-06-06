# Fileconvy Server

Fileconvy Server is the backend API for Fileconvy. It currently provides a small HTTP service for checking server health and merging PDF files.

## Scope

This repository contains the Go backend only. It does not include a frontend, deployment configuration, authentication, user accounts, file storage, or background jobs.

Current API features:

- `GET /ping` returns a basic health response.
- `POST /api/pdf/merge` accepts multiple PDF uploads and returns a merged PDF file.

The PDF merge endpoint validates that uploaded files have a `.pdf` extension and start with a PDF file header before merging them.

## Requirements

- Go `1.26.3` or newer, matching the version in `go.mod`
- Git
- Network access the first time dependencies are downloaded
- A system temporary directory that the server can write to

Main Go dependencies:

- `github.com/gin-gonic/gin` for the HTTP server
- `github.com/gin-contrib/cors` for CORS handling
- `github.com/pdfcpu/pdfcpu` for PDF merging

## Setup

Clone the repository and enter the project directory:

```bash
git clone <repository-url>
cd fileconvy-server
```

Download Go dependencies:

```bash
go mod download
```

## Run The Server

Start the API server:

```bash
go run ./cmd/server
```

By default, the server listens on:

```text
http://localhost:8080
```

The port is currently hard-coded in `cmd/server/main.go`.

## Check That It Works

Health check:

```bash
curl http://localhost:8080/ping
```

Expected response:

```json
{"message":"pong"}
```

Merge PDF files:

```bash
curl -X POST http://localhost:8080/api/pdf/merge \
  -F "files=@first.pdf" \
  -F "files=@second.pdf" \
  --output merged.pdf
```

The merge endpoint requires at least two PDF files. The response is an `application/pdf` download named `merged.pdf`.

## Frontend Access

CORS is configured for local frontend development from:

- `http://localhost:3000`
- `http://localhost:5173`

If a frontend runs from another origin, update the allowed origins in `internal/server/router.go`.

## Development

Run all tests:

```bash
go test ./...
```

Format Go files:

```bash
gofmt -w cmd internal
```

Build the server binary:

```bash
go build -o fileconvy-server ./cmd/server
```

Run the built binary:

```bash
./fileconvy-server
```

## API Reference

### `GET /ping`

Returns server health.

Response:

```json
{"message":"pong"}
```

### `POST /api/pdf/merge`

Merges uploaded PDF files into a single PDF.

Request type:

```text
multipart/form-data
```

Preferred file field:

```text
files
```

Rules:

- Upload at least two files.
- Each file must use the `.pdf` extension.
- Each file must contain a valid PDF header.

Success response:

- Status: `200 OK`
- Content-Type: `application/pdf`
- Content-Disposition: `attachment; filename="merged.pdf"`

Common error responses:

- `400 Bad Request` when the request is not multipart form data
- `400 Bad Request` when fewer than two PDF files are uploaded
- `400 Bad Request` when an uploaded file is not a PDF
- `500 Internal Server Error` when temporary storage or PDF merging fails

## Operational Notes

Uploaded PDFs are written to a temporary directory while a request is being processed. The temporary directory is removed after the request finishes.

The server currently accepts multipart uploads up to `64 MiB` in memory before Gin spills file data as needed. Very large files may require additional limits, streaming behavior, request timeouts, and reverse proxy configuration before production use.
