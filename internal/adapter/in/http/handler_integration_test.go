package http

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	archiveapp "github.com/Viespirkiu-grupe/goviesdezeproxyrewrite/internal/app/archive"
	"github.com/Viespirkiu-grupe/goviesdezeproxyrewrite/internal/port/out"
	"github.com/go-chi/chi/v5"
)

type testProxyInfoGateway struct {
	res out.ProxyInfoResponse
	err error

	lastRequestedID string
}

func (g *testProxyInfoGateway) FetchProxyInfo(_ context.Context, requestedID string) (out.ProxyInfoResponse, error) {
	g.lastRequestedID = requestedID
	return g.res, g.err
}

type testFileGateway struct {
	res         out.FileResponse
	err         error
	lastHeaders map[string]string
}

func (g *testFileGateway) FetchFile(_ context.Context, _ string, headers map[string]string) (out.FileResponse, error) {
	g.lastHeaders = make(map[string]string, len(headers))
	for k, v := range headers {
		g.lastHeaders[k] = v
	}
	return g.res, g.err
}

type testArchiveGateway struct {
	listFilesFn            func([]byte) ([]string, error)
	extractFileFn          func([]byte, string) (io.ReadCloser, error)
	extractEmlAttachmentFn func([]byte, string, string) (io.ReadCloser, error)
	convertMsgToEmlFn      func([]byte) ([]byte, error)
}

func (g *testArchiveGateway) ListFiles(archiveBytes []byte) ([]string, error) {
	if g.listFilesFn != nil {
		return g.listFilesFn(archiveBytes)
	}
	return nil, nil
}

func (g *testArchiveGateway) ExtractFile(archiveBytes []byte, filename string) (io.ReadCloser, error) {
	if g.extractFileFn != nil {
		return g.extractFileFn(archiveBytes, filename)
	}
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (g *testArchiveGateway) ExtractEmlAttachment(in []byte, filename, idx string) (io.ReadCloser, error) {
	if g.extractEmlAttachmentFn != nil {
		return g.extractEmlAttachmentFn(in, filename, idx)
	}
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (g *testArchiveGateway) ConvertMsgToEml(in []byte) ([]byte, error) {
	if g.convertMsgToEmlFn != nil {
		return g.convertMsgToEmlFn(in)
	}
	return nil, nil
}

type testConversionGateway struct {
	convertFn func(context.Context, io.Reader, string, string) (io.ReadCloser, string, string, error)
}

func (g *testConversionGateway) Convert(ctx context.Context, src io.Reader, sourceName, targetFormat string) (io.ReadCloser, string, string, error) {
	if g.convertFn != nil {
		return g.convertFn(ctx, src, sourceName, targetFormat)
	}
	return io.NopCloser(bytes.NewReader(nil)), sourceName, "application/octet-stream", nil
}

func buildHTTPAdapter(proxyInfo out.ProxyInfoGateway, file out.FileGateway, archive out.ArchiveGateway, conversion out.ConversionGateway) http.Handler {
	svc := archiveapp.NewService(proxyInfo, file, archive, conversion)
	h := NewHandler(svc)
	r := chi.NewRouter()
	r.Get("/{dokId:[0-9]+}/{fileId:[0-9]+}", h.HandleArchive)
	r.Get("/{dokId:[0-9]+}/{fileId:[0-9]+}/*", h.HandleArchive)
	r.Get("/{id:[0-9a-fA-F]{32}|[0-9]+}", h.HandleArchive)
	r.Get("/{id:[0-9a-fA-F]{32}|[0-9]+}/*", h.HandleArchive)
	return r
}

func TestHTTPAdapter_FullFlowWithRouteAndQuery(t *testing.T) {
	t.Parallel()

	proxyInfo := &testProxyInfoGateway{
		res: out.ProxyInfoResponse{StatusCode: http.StatusOK, Body: []byte(`{"fileUrl":"http://upstream/file","extension":"zip","fileName":"my file.txt"}`)},
	}
	file := &testFileGateway{
		res: out.FileResponse{
			StatusCode: http.StatusOK,
			Headers:    make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ARCHIVE")),
		},
	}
	archive := &testArchiveGateway{
		listFilesFn: func(_ []byte) ([]string, error) {
			return []string{"folder/target file.txt"}, nil
		},
		extractFileFn: func(_ []byte, filename string) (io.ReadCloser, error) {
			if filename != "folder/target file.txt" {
				t.Fatalf("unexpected selected archive file: %s", filename)
			}
			return io.NopCloser(strings.NewReader("RAW-BODY")), nil
		},
	}
	conversion := &testConversionGateway{
		convertFn: func(_ context.Context, _ io.Reader, sourceName, targetFormat string) (io.ReadCloser, string, string, error) {
			if sourceName != "folder/target file.txt" {
				t.Fatalf("unexpected conversion source name: %s", sourceName)
			}
			if targetFormat != "pdf" {
				t.Fatalf("unexpected convertTo: %s", targetFormat)
			}
			return io.NopCloser(strings.NewReader("PDF-BODY")), "converted report.pdf", "application/pdf", nil
		},
	}

	r := buildHTTPAdapter(proxyInfo, file, archive, conversion)
	req := httptest.NewRequest(http.MethodGet, "/12/34/target%20file.txt?convertTo=pdf&index=2", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if proxyInfo.lastRequestedID != "12/34" {
		t.Fatalf("expected requestedID 12/34, got %s", proxyInfo.lastRequestedID)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/pdf" {
		t.Fatalf("expected content-type application/pdf, got %s", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != "inline; filename*=UTF-8''converted%20report.pdf" {
		t.Fatalf("unexpected content-disposition: %s", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=2592000, immutable" {
		t.Fatalf("unexpected cache-control: %s", got)
	}
	if body := rec.Body.String(); body != "PDF-BODY" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestHTTPAdapter_ReturnsStatusErrorFromService(t *testing.T) {
	t.Parallel()

	r := buildHTTPAdapter(
		&testProxyInfoGateway{res: out.ProxyInfoResponse{StatusCode: http.StatusOK, Body: []byte(`{"fileUrl":""}`)}},
		&testFileGateway{},
		&testArchiveGateway{},
		&testConversionGateway{},
	)

	req := httptest.NewRequest(http.MethodGet, "/123", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "proxy info missing fileUrl") {
		t.Fatalf("unexpected body: %q", rec.Body.String())
	}
}

func TestHTTPAdapter_PassesThroughUpstreamNon2xx(t *testing.T) {
	t.Parallel()

	r := buildHTTPAdapter(
		&testProxyInfoGateway{res: out.ProxyInfoResponse{StatusCode: http.StatusOK, Body: []byte(`{"fileUrl":"http://upstream/file"}`)}},
		&testFileGateway{res: out.FileResponse{StatusCode: http.StatusNotFound, Headers: make(http.Header), Body: io.NopCloser(strings.NewReader("missing"))}},
		&testArchiveGateway{},
		&testConversionGateway{},
	)

	req := httptest.NewRequest(http.MethodGet, "/123", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}
	if rec.Body.String() != "missing" {
		t.Fatalf("unexpected body: %q", rec.Body.String())
	}
}

func TestHTTPAdapter_MD5RouteUsesIDParam(t *testing.T) {
	t.Parallel()

	proxyInfo := &testProxyInfoGateway{
		res: out.ProxyInfoResponse{StatusCode: http.StatusOK, Body: []byte(`{"fileUrl":"http://upstream/file","fileName":"a.txt"}`)},
	}

	r := buildHTTPAdapter(
		proxyInfo,
		&testFileGateway{res: out.FileResponse{StatusCode: http.StatusOK, Headers: make(http.Header), Body: io.NopCloser(strings.NewReader("ok"))}},
		&testArchiveGateway{},
		&testConversionGateway{},
	)

	md5ID := "0123456789abcdef0123456789ABCDEF"
	req := httptest.NewRequest(http.MethodGet, "/"+md5ID, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if proxyInfo.lastRequestedID != md5ID {
		t.Fatalf("expected requestedID %s, got %s", md5ID, proxyInfo.lastRequestedID)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("unexpected body: %q", rec.Body.String())
	}
}

func TestHTTPAdapter_PathFileZeroStreamsRawFile(t *testing.T) {
	t.Parallel()

	archiveCalled := false

	r := buildHTTPAdapter(
		&testProxyInfoGateway{res: out.ProxyInfoResponse{StatusCode: http.StatusOK, Body: []byte(`{"fileUrl":"http://upstream/file","extension":"zip","fileName":"raw.zip"}`)}},
		&testFileGateway{res: out.FileResponse{StatusCode: http.StatusOK, Headers: make(http.Header), Body: io.NopCloser(strings.NewReader("raw-content"))}},
		&testArchiveGateway{listFilesFn: func(_ []byte) ([]string, error) {
			archiveCalled = true
			return []string{"x"}, nil
		}},
		&testConversionGateway{},
	)

	req := httptest.NewRequest(http.MethodGet, "/123/0", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if archiveCalled {
		t.Fatal("archive path should not be used for /0")
	}
	if rec.Body.String() != "raw-content" {
		t.Fatalf("unexpected body: %q", rec.Body.String())
	}
}

func TestHTTPAdapter_InvalidConvertToReturnsBadRequest(t *testing.T) {
	t.Parallel()

	r := buildHTTPAdapter(
		&testProxyInfoGateway{res: out.ProxyInfoResponse{StatusCode: http.StatusOK, Body: []byte(`{"fileUrl":"http://upstream/file","fileName":"file.pdf"}`)}},
		&testFileGateway{res: out.FileResponse{StatusCode: http.StatusOK, Headers: make(http.Header), Body: io.NopCloser(strings.NewReader("content"))}},
		&testArchiveGateway{},
		&testConversionGateway{},
	)

	req := httptest.NewRequest(http.MethodGet, "/123?convertTo=undefined", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unsupported convertTo value") {
		t.Fatalf("unexpected body: %q", rec.Body.String())
	}
}

func TestHTTPAdapter_AppliesRangeOnResponse(t *testing.T) {
	t.Parallel()

	fileGateway := &testFileGateway{res: out.FileResponse{StatusCode: http.StatusOK, Headers: make(http.Header), Body: io.NopCloser(strings.NewReader("0123456789abcdef"))}}

	r := buildHTTPAdapter(
		&testProxyInfoGateway{res: out.ProxyInfoResponse{StatusCode: http.StatusOK, Body: []byte(`{"fileUrl":"http://upstream/file","fileName":"file.pdf"}`)}},
		fileGateway,
		&testArchiveGateway{},
		&testConversionGateway{},
	)

	req := httptest.NewRequest(http.MethodGet, "/123", nil)
	req.Header.Set("Range", "bytes=0-10")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected status 206, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 0-10/16" {
		t.Fatalf("unexpected content-range: %s", got)
	}
	if rec.Body.String() != "0123456789a" {
		t.Fatalf("unexpected partial body: %q", rec.Body.String())
	}
	if got := fileGateway.lastHeaders["Range"]; got != "" {
		t.Fatalf("expected no upstream Range forwarding, got %q", got)
	}
}
