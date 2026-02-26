package utils

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Viespirkiu-grupe/goviesdezeproxyrewrite/pkg/sconv"
)

func getTimeout() time.Duration {
	timeoutStr := os.Getenv("PDF_CONVERT_TIMEOUT")
	if timeoutStr == "" {
		return 30 * time.Second
	}
	timeoutInt := sconv.String(timeoutStr).Int()
	if timeoutInt <= 0 {
		return 30 * time.Second
	}
	return time.Duration(timeoutInt) * time.Second
}

func workersCount() int {
	count := sconv.String(os.Getenv("PDF_WORKERS_DOC_COUNT")).Int()
	if count == 0 {
		return 1
	}
	return count
}

var semaphoreDoc = make(chan struct{}, workersCount())

func ConvertDocumentReaderToPDF(
	w http.ResponseWriter,
	r *http.Request,
	src io.Reader,
	origName string,
	status int,
) error {
	semaphoreDoc <- struct{}{}
	defer func() { <-semaphoreDoc }()
	tmpIn, err := os.CreateTemp("", "archive-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmpIn.Name())

	if _, err := io.Copy(tmpIn, src); err != nil {
		tmpIn.Close()
		return err
	}
	tmpIn.Close()

	ctx, cancel := context.WithTimeout(r.Context(), getTimeout())
	defer cancel()
	var mu atomic.Bool
	outDir := os.TempDir()
	cmd := exec.CommandContext(
		ctx,
		"libreoffice",
		"--headless",
		"--convert-to", "pdf",
		"--outdir", outDir,
		tmpIn.Name(),
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if mu.Load() || cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	go io.Copy(os.Stdout, stdout)
	go io.Copy(os.Stderr, stderr)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start libreoffice: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		mu.Store(true)
		return fmt.Errorf("libreoffice process error: %w", err)
	}
	mu.Store(true)

	pdfPath := filepath.Join(
		outDir,
		strings.TrimSuffix(filepath.Base(tmpIn.Name()), filepath.Ext(tmpIn.Name()))+".pdf",
	)
	defer os.Remove(pdfPath)

	f, err := os.Open(pdfPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if origName != "" {
		fn := url.PathEscape(strings.TrimSuffix(origName, filepath.Ext(origName)))
		w.Header().Set("Content-Disposition",
			fmt.Sprintf("inline; filename*=UTF-8''%s.pdf", fn))
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Cache-Control", "public, max-age=2592000, immutable")
	w.WriteHeader(status)
	_, err = io.Copy(w, f)
	return err
}

func imageWorkersCount() int {
	count := sconv.String(os.Getenv("PDF_WORKERS_IMG_COUNT")).Int()
	if count < 1 {
		return 1
	}
	return count
}

var semaphoreImg = make(chan struct{}, imageWorkersCount())

func ConvertImageReaderToPDF(
	w http.ResponseWriter,
	r *http.Request,
	src io.Reader,
	origName string,
	status int,
) error {
	semaphoreImg <- struct{}{}        // acquire slot
	defer func() { <-semaphoreImg }() // release slot

	// Save the image to a temp file
	tmpIn, err := os.CreateTemp("", "image-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmpIn.Name())

	if _, err := io.Copy(tmpIn, src); err != nil {
		tmpIn.Close()
		return err
	}
	tmpIn.Close()

	// Prepare output PDF path
	tmpOutDir := os.TempDir()
	baseName := strings.TrimSuffix(filepath.Base(tmpIn.Name()), filepath.Ext(tmpIn.Name()))
	pdfPath := filepath.Join(tmpOutDir, baseName+".pdf")

	ctx, cancel := context.WithTimeout(r.Context(), getTimeout())
	defer cancel()

	// Convert image to PDF using ImageMagick
	cmd := exec.CommandContext(
		ctx,
		"convert",
		"-limit", "memory", "500MB",
		"-limit", "map", "1GB",
		"-limit", "thread", "8",
		tmpIn.Name(),
		"-units", "PixelsPerInch",
		"-density", "300",
		"-resize", "2480x3508",
		"-background", "white",
		"-extent", "2480x3508",
		"-compress", "Zip",
		pdfPath,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ImageMagick conversion failed: %w: %s", err, output)
	}
	defer os.Remove(pdfPath)

	// Open the resulting PDF
	f, err := os.Open(pdfPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// Set headers
	if origName != "" {
		fn := url.PathEscape(strings.TrimSuffix(origName, filepath.Ext(origName)))
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename*=UTF-8''%s.pdf", fn))
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Cache-Control", "public, max-age=2592000, immutable")
	w.WriteHeader(status)

	_, err = io.Copy(w, f)
	return err
}

// ConvertImageReader converts an image from src to the target format
// supported formats: jpg, jpeg, png, tif, tiff, bmp, prn, gif, jfif, heic
func ConvertImageReader(src io.Reader, targetFormat string) (io.ReadCloser, error) {
	// Save input to temp file
	tmpIn, err := os.CreateTemp("", "img-*")
	if err != nil {
		return nil, err
	}
	defer tmpIn.Close()

	if _, err := io.Copy(tmpIn, src); err != nil {
		return nil, err
	}

	// Prepare output temp file
	base := strings.TrimSuffix(filepath.Base(tmpIn.Name()), filepath.Ext(tmpIn.Name()))
	tmpOut, err := os.CreateTemp("", base+"-*."+strings.ToLower(targetFormat))
	if err != nil {
		return nil, err
	}
	tmpOut.Close() // will be written by magick

	ctx, cancel := context.WithTimeout(context.Background(), getTimeout())
	defer cancel()

	// Use ImageMagick to convert
	cmd := exec.CommandContext(ctx, "convert", tmpIn.Name(), tmpOut.Name())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	output, err := cmd.CombinedOutput()
	if err != nil {
		os.Remove(tmpOut.Name())
		return nil, fmt.Errorf("ImageMagick conversion failed: %w: %s", err, output)
	}

	// Open converted file for reading
	f, err := os.Open(tmpOut.Name())
	if err != nil {
		os.Remove(tmpOut.Name())
		return nil, err
	}

	// Wrap in ReadCloser that removes file on close
	rc := &tempFileReadCloser{f, tmpOut.Name()}
	return rc, nil
}

// tempFileReadCloser removes file when closed
type tempFileReadCloser struct {
	*os.File
	path string
}

func (t *tempFileReadCloser) Close() error {
	err1 := t.File.Close()
	err2 := os.Remove(t.path)
	if err1 != nil {
		return err1
	}
	return err2
}
