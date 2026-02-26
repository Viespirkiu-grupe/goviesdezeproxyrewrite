package out

import (
	"context"
	"io"
	"net/http"
)

type ProxyInfoResponse struct {
	StatusCode int
	Body       []byte
}

type FileResponse struct {
	StatusCode int
	Headers    http.Header
	Body       io.ReadCloser
}

type ProxyInfoGateway interface {
	FetchProxyInfo(ctx context.Context, requestedID string) (ProxyInfoResponse, error)
}

type FileGateway interface {
	FetchFile(ctx context.Context, fileURL string, headers map[string]string) (FileResponse, error)
}

type ArchiveGateway interface {
	ListFiles(archiveBytes []byte) ([]string, error)
	ExtractFile(archiveBytes []byte, filename string) (io.ReadCloser, error)
	ExtractEmlAttachment(in []byte, filename, idx string) (io.ReadCloser, error)
	ConvertMsgToEml(in []byte) ([]byte, error)
}

type ConversionGateway interface {
	Convert(ctx context.Context, src io.Reader, sourceName, targetFormat string) (io.ReadCloser, string, string, error)
}
