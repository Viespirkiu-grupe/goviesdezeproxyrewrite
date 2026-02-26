package converter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"

	"github.com/Viespirkiu-grupe/goviesdezeproxyrewrite/utils"
)

type Gateway struct{}

func NewGateway() *Gateway {
	return &Gateway{}
}

func (g *Gateway) Convert(ctx context.Context, src io.Reader, sourceName, targetFormat string) (io.ReadCloser, string, string, error) {
	targetFormat = strings.ToLower(targetFormat)
	sourceExt := strings.TrimPrefix(strings.ToLower(filepath.Ext(sourceName)), ".")

	switch {
	case targetFormat == "pdf":
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		var err error
		if isImage(sourceExt) {
			err = utils.ConvertImageReaderToPDF(rec, req, src, sourceName, http.StatusOK)
		} else {
			err = utils.ConvertDocumentReaderToPDF(rec, req, src, sourceName, http.StatusOK)
		}
		if err != nil {
			return nil, "", "", err
		}
		name := strings.TrimSuffix(sourceName, filepath.Ext(sourceName)) + ".pdf"
		return io.NopCloser(bytes.NewReader(rec.Body.Bytes())), name, "application/pdf", nil

	case isImage(targetFormat):
		out, err := utils.ConvertImageReader(src, targetFormat)
		if err != nil {
			return nil, "", "", err
		}
		name := strings.TrimSuffix(sourceName, filepath.Ext(sourceName)) + "." + targetFormat
		return out, name, "image/" + targetFormat, nil
	default:
		return nil, "", "", fmt.Errorf("unsupported conversion target: %s", targetFormat)
	}
}

func isImage(ext string) bool {
	switch ext {
	case "jpg", "jpeg", "png", "tif", "tiff", "bmp", "prn", "gif", "jfif", "heic":
		return true
	default:
		return false
	}
}
