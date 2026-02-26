package ziputil

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"
)

func TestNormalizeArchivePath(t *testing.T) {
	t.Parallel()

	got := normalizeArchivePath("./deep\\nested/file.pdf")
	if got != "deep/nested/file.pdf" {
		t.Fatalf("unexpected normalized path: %s", got)
	}
}

func TestGetFileFromArchiveV3_NotFoundReturnsError(t *testing.T) {
	t.Parallel()

	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	w, err := zw.Create("deep/nested/found.pdf")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := io.WriteString(w, "ok"); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	_, err = GetFileFromArchiveV3(buf.Bytes(), "missing.pdf")
	if err == nil {
		t.Fatal("expected not found error")
	}
}
