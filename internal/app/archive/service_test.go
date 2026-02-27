package archive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/Viespirkiu-grupe/goviesdezeproxyrewrite/internal/port/out"
)

type proxyInfoGatewayMock struct {
	res out.ProxyInfoResponse
	err error
}

func (m *proxyInfoGatewayMock) FetchProxyInfo(_ context.Context, _ string) (out.ProxyInfoResponse, error) {
	return m.res, m.err
}

type fileGatewayMock struct {
	res         out.FileResponse
	err         error
	lastHeaders map[string]string
	allHeaders  []map[string]string
	responses   []out.FileResponse
	callCount   int
}

func (m *fileGatewayMock) FetchFile(_ context.Context, _ string, headers map[string]string) (out.FileResponse, error) {
	m.callCount++
	m.lastHeaders = make(map[string]string, len(headers))
	for k, v := range headers {
		m.lastHeaders[k] = v
	}
	m.allHeaders = append(m.allHeaders, m.lastHeaders)
	if len(m.responses) >= m.callCount {
		return m.responses[m.callCount-1], m.err
	}
	return m.res, m.err
}

type archiveGatewayMock struct {
	listFilesFn            func([]byte) ([]string, error)
	extractFileFn          func([]byte, string) (io.ReadCloser, error)
	extractEmlAttachmentFn func([]byte, string, string) (io.ReadCloser, error)
	convertMsgToEmlFn      func([]byte) ([]byte, error)
}

func (m *archiveGatewayMock) ListFiles(b []byte) ([]string, error) {
	if m.listFilesFn != nil {
		return m.listFilesFn(b)
	}
	return nil, errors.New("not implemented")
}

func (m *archiveGatewayMock) ExtractFile(b []byte, s string) (io.ReadCloser, error) {
	if m.extractFileFn != nil {
		return m.extractFileFn(b, s)
	}
	return nil, errors.New("not implemented")
}

func (m *archiveGatewayMock) ExtractEmlAttachment(b []byte, s, idx string) (io.ReadCloser, error) {
	if m.extractEmlAttachmentFn != nil {
		return m.extractEmlAttachmentFn(b, s, idx)
	}
	return nil, errors.New("not implemented")
}

func (m *archiveGatewayMock) ConvertMsgToEml(b []byte) ([]byte, error) {
	if m.convertMsgToEmlFn != nil {
		return m.convertMsgToEmlFn(b)
	}
	return nil, errors.New("not implemented")
}

type conversionGatewayMock struct {
	convertFn func(context.Context, io.Reader, string, string) (io.ReadCloser, string, string, error)
}

func (m *conversionGatewayMock) Convert(ctx context.Context, r io.Reader, sourceName, targetFormat string) (io.ReadCloser, string, string, error) {
	if m.convertFn != nil {
		return m.convertFn(ctx, r, sourceName, targetFormat)
	}
	return nil, "", "", errors.New("not implemented")
}

func TestExecute_PassthroughProxyInfoErrors(t *testing.T) {
	t.Parallel()

	svc := NewService(
		&proxyInfoGatewayMock{res: out.ProxyInfoResponse{StatusCode: 401, Body: []byte("denied")}},
		&fileGatewayMock{},
		&archiveGatewayMock{},
		&conversionGatewayMock{},
	)

	res, err := svc.Execute(context.Background(), Request{RequestedID: "123"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("expected status 401, got %d", res.StatusCode)
	}
	b, _ := io.ReadAll(res.Body)
	if string(b) != "denied" {
		t.Fatalf("unexpected body: %q", string(b))
	}
}

func TestExecute_MissingFileURLReturnsBadGateway(t *testing.T) {
	t.Parallel()

	svc := NewService(
		&proxyInfoGatewayMock{res: out.ProxyInfoResponse{StatusCode: 200, Body: []byte(`{"fileUrl":""}`)}},
		&fileGatewayMock{},
		&archiveGatewayMock{},
		&conversionGatewayMock{},
	)

	_, err := svc.Execute(context.Background(), Request{RequestedID: "123"})
	if err == nil {
		t.Fatal("expected error")
	}
	statusErr := &StatusError{}
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Status != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d", statusErr.Status)
	}
}

func TestExecute_ArchiveExtractionSelectsBestMatch(t *testing.T) {
	t.Parallel()

	proxyBody := []byte(`{"fileUrl":"http://example/file","extension":"zip","fileName":"orig.zip"}`)

	svc := NewService(
		&proxyInfoGatewayMock{res: out.ProxyInfoResponse{StatusCode: 200, Body: proxyBody}},
		&fileGatewayMock{res: out.FileResponse{
			StatusCode: 200,
			Headers:    make(http.Header),
			Body:       io.NopCloser(bytes.NewReader([]byte("archive-bytes"))),
		}},
		&archiveGatewayMock{
			listFilesFn: func(_ []byte) ([]string, error) {
				return []string{"folder/invoice_2024.pdf", "folder/note.txt"}, nil
			},
			extractFileFn: func(_ []byte, filename string) (io.ReadCloser, error) {
				if filename != "folder/invoice_2024.pdf" {
					t.Fatalf("unexpected chosen file: %s", filename)
				}
				return io.NopCloser(bytes.NewReader([]byte("pdf-data"))), nil
			},
		},
		&conversionGatewayMock{},
	)

	res, err := svc.Execute(context.Background(), Request{RequestedID: "123", PathFile: "invoice_2024.pdf"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	defer res.Body.Close()
	if res.FileName != "folder/invoice_2024.pdf" {
		t.Fatalf("unexpected filename: %s", res.FileName)
	}
	b, _ := io.ReadAll(res.Body)
	if string(b) != "pdf-data" {
		t.Fatalf("unexpected body: %q", string(b))
	}
}

func TestExecute_ImageTargetForNonImageReturnsBadRequest(t *testing.T) {
	t.Parallel()

	proxyBody := []byte(`{"fileUrl":"http://example/file","fileName":"report.docx"}`)

	svc := NewService(
		&proxyInfoGatewayMock{res: out.ProxyInfoResponse{StatusCode: 200, Body: proxyBody}},
		&fileGatewayMock{res: out.FileResponse{
			StatusCode: 200,
			Headers:    make(http.Header),
			Body:       io.NopCloser(bytes.NewReader([]byte("doc-bytes"))),
		}},
		&archiveGatewayMock{},
		&conversionGatewayMock{},
	)

	_, err := svc.Execute(context.Background(), Request{RequestedID: "123", ConvertTo: "png"})
	if err == nil {
		t.Fatal("expected error")
	}
	statusErr := &StatusError{}
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Status != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", statusErr.Status)
	}
}

func TestBestMatch_Threshold(t *testing.T) {
	t.Parallel()

	_, err := bestMatch("invoice.pdf", []string{"abc.txt", "xyz.bin"})
	if err == nil {
		t.Fatal("expected error when similarity is too low")
	}
}

func TestBestMatch_ExactBaseNameWins(t *testing.T) {
	t.Parallel()

	got, err := bestMatch("/inbox/22359029.pdf", []string{"deep/other.txt", "deep/nested/22359029.pdf"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != "deep/nested/22359029.pdf" {
		t.Fatalf("unexpected match: %s", got)
	}
}

func TestExecute_PathFileZeroSkipsExtraction(t *testing.T) {
	t.Parallel()

	proxyBody := []byte(`{"fileUrl":"http://example/file","extension":"zip","fileName":"orig.zip"}`)

	listCalled := false
	svc := NewService(
		&proxyInfoGatewayMock{res: out.ProxyInfoResponse{StatusCode: 200, Body: proxyBody}},
		&fileGatewayMock{res: out.FileResponse{
			StatusCode: 200,
			Headers:    make(http.Header),
			Body:       io.NopCloser(bytes.NewReader([]byte("raw-zip-bytes"))),
		}},
		&archiveGatewayMock{
			listFilesFn: func(_ []byte) ([]string, error) {
				listCalled = true
				return nil, nil
			},
		},
		&conversionGatewayMock{},
	)

	res, err := svc.Execute(context.Background(), Request{RequestedID: "123", PathFile: "0"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	defer res.Body.Close()

	if listCalled {
		t.Fatal("expected archive listing to be skipped for pathFile=0")
	}

	b, _ := io.ReadAll(res.Body)
	if string(b) != "raw-zip-bytes" {
		t.Fatalf("unexpected body: %q", string(b))
	}
}

func TestExecute_UnsupportedConvertToReturnsBadRequest(t *testing.T) {
	t.Parallel()

	proxyBody := []byte(`{"fileUrl":"http://example/file","fileName":"report.png"}`)

	svc := NewService(
		&proxyInfoGatewayMock{res: out.ProxyInfoResponse{StatusCode: 200, Body: proxyBody}},
		&fileGatewayMock{res: out.FileResponse{
			StatusCode: 200,
			Headers:    make(http.Header),
			Body:       io.NopCloser(bytes.NewReader([]byte("pdf-bytes"))),
		}},
		&archiveGatewayMock{},
		&conversionGatewayMock{},
	)

	_, err := svc.Execute(context.Background(), Request{RequestedID: "123", ConvertTo: "randomtext"})
	if err == nil {
		t.Fatal("expected error")
	}

	statusErr := &StatusError{}
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Status != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", statusErr.Status)
	}
}

func TestExecute_WebpConvertToIsAllowed(t *testing.T) {
	t.Parallel()

	proxyBody := []byte(`{"fileUrl":"http://example/file","fileName":"image.png"}`)
	conversionCalled := false

	svc := NewService(
		&proxyInfoGatewayMock{res: out.ProxyInfoResponse{StatusCode: 200, Body: proxyBody}},
		&fileGatewayMock{res: out.FileResponse{
			StatusCode: 200,
			Headers:    make(http.Header),
			Body:       io.NopCloser(bytes.NewReader([]byte("img-bytes"))),
		}},
		&archiveGatewayMock{},
		&conversionGatewayMock{convertFn: func(_ context.Context, _ io.Reader, _ string, targetFormat string) (io.ReadCloser, string, string, error) {
			conversionCalled = true
			if targetFormat != "webp" {
				t.Fatalf("expected webp target, got %s", targetFormat)
			}
			return io.NopCloser(bytes.NewReader([]byte("webp-data"))), "image.webp", "image/webp", nil
		}},
	)

	res, err := svc.Execute(context.Background(), Request{RequestedID: "123", ConvertTo: "webp"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	defer res.Body.Close()

	if !conversionCalled {
		t.Fatal("expected conversion to be called")
	}
	if res.ContentType != "image/webp" {
		t.Fatalf("expected image/webp content type, got %s", res.ContentType)
	}
}

func TestExecute_UsesProxyInfoHeadersForUpstream(t *testing.T) {
	t.Parallel()

	proxyBody := []byte(`{"fileUrl":"http://example/file","fileName":"report.pdf","headers":{"X-Test":"1"}}`)
	fileGateway := &fileGatewayMock{res: out.FileResponse{
		StatusCode: 200,
		Headers:    make(http.Header),
		Body:       io.NopCloser(bytes.NewReader([]byte("partial"))),
	}}

	svc := NewService(
		&proxyInfoGatewayMock{res: out.ProxyInfoResponse{StatusCode: 200, Body: proxyBody}},
		fileGateway,
		&archiveGatewayMock{},
		&conversionGatewayMock{},
	)

	res, err := svc.Execute(context.Background(), Request{RequestedID: "123"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	defer res.Body.Close()

	if got := fileGateway.lastHeaders["X-Test"]; got != "1" {
		t.Fatalf("expected original header preserved, got %q", got)
	}
}

func TestExecute_ForwardsContentLengthForRawPassthrough(t *testing.T) {
	t.Parallel()

	proxyBody := []byte(`{"fileUrl":"http://example/file","fileName":"report.pdf"}`)
	fileGateway := &fileGatewayMock{res: out.FileResponse{
		StatusCode: 200,
		Headers:    http.Header{"Content-Length": []string{"1234"}},
		Body:       io.NopCloser(bytes.NewReader([]byte("body"))),
	}}

	svc := NewService(
		&proxyInfoGatewayMock{res: out.ProxyInfoResponse{StatusCode: 200, Body: proxyBody}},
		fileGateway,
		&archiveGatewayMock{},
		&conversionGatewayMock{},
	)

	res, err := svc.Execute(context.Background(), Request{RequestedID: "123"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	defer res.Body.Close()

	if got := res.ForwardContentLength; got != "1234" {
		t.Fatalf("expected content-length 1234, got %q", got)
	}
}

func TestExecute_DoesNotForwardContentLengthForConversion(t *testing.T) {
	t.Parallel()

	proxyBody := []byte(`{"fileUrl":"http://example/file","fileName":"report.png"}`)
	fileGateway := &fileGatewayMock{res: out.FileResponse{
		StatusCode: 200,
		Headers:    http.Header{"Content-Length": []string{"9999"}},
		Body:       io.NopCloser(bytes.NewReader([]byte("raw"))),
	}}

	svc := NewService(
		&proxyInfoGatewayMock{res: out.ProxyInfoResponse{StatusCode: 200, Body: proxyBody}},
		fileGateway,
		&archiveGatewayMock{},
		&conversionGatewayMock{convertFn: func(_ context.Context, _ io.Reader, _ string, _ string) (io.ReadCloser, string, string, error) {
			return io.NopCloser(bytes.NewReader([]byte("converted"))), "report.webp", "image/webp", nil
		}},
	)

	res, err := svc.Execute(context.Background(), Request{RequestedID: "123", ConvertTo: "webp"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	defer res.Body.Close()

	if got := res.ForwardContentLength; got != "" {
		t.Fatalf("expected empty forwarded content-length, got %q", got)
	}
}

func TestExecute_ForwardsRangeForRawPassthrough(t *testing.T) {
	t.Parallel()

	proxyBody := []byte(`{"fileUrl":"http://example/file","fileName":"report.png"}`)
	fileGateway := &fileGatewayMock{res: out.FileResponse{
		StatusCode: http.StatusPartialContent,
		Headers: http.Header{
			"Content-Range": []string{"bytes 0-10/100"},
		},
		Body: io.NopCloser(bytes.NewReader([]byte("partial"))),
	}}

	svc := NewService(
		&proxyInfoGatewayMock{res: out.ProxyInfoResponse{StatusCode: 200, Body: proxyBody}},
		fileGateway,
		&archiveGatewayMock{},
		&conversionGatewayMock{},
	)

	res, err := svc.Execute(context.Background(), Request{RequestedID: "123", Range: "bytes=0-10", IfRange: "W/\"etag\""})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	defer res.Body.Close()

	if got := fileGateway.lastHeaders["Range"]; got != "bytes=0-10" {
		t.Fatalf("expected Range forwarded, got %q", got)
	}
	if got := fileGateway.lastHeaders["If-Range"]; got != "W/\"etag\"" {
		t.Fatalf("expected If-Range forwarded, got %q", got)
	}
	if got := res.ForwardContentRange; got != "bytes 0-10/100" {
		t.Fatalf("expected content-range in result, got %q", got)
	}
}

func TestExecute_DoesNotForwardRangeForConversion(t *testing.T) {
	t.Parallel()

	proxyBody := []byte(`{"fileUrl":"http://example/file","fileName":"report.png"}`)
	fileGateway := &fileGatewayMock{res: out.FileResponse{
		StatusCode: 200,
		Headers:    make(http.Header),
		Body:       io.NopCloser(bytes.NewReader([]byte("full"))),
	}}

	svc := NewService(
		&proxyInfoGatewayMock{res: out.ProxyInfoResponse{StatusCode: 200, Body: proxyBody}},
		fileGateway,
		&archiveGatewayMock{},
		&conversionGatewayMock{convertFn: func(_ context.Context, _ io.Reader, _ string, _ string) (io.ReadCloser, string, string, error) {
			return io.NopCloser(bytes.NewReader([]byte("converted"))), "report.webp", "image/webp", nil
		}},
	)

	res, err := svc.Execute(context.Background(), Request{RequestedID: "123", ConvertTo: "webp", Range: "bytes=0-10"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	defer res.Body.Close()

	if got := fileGateway.lastHeaders["Range"]; got != "" {
		t.Fatalf("expected no Range forwarding for conversion, got %q", got)
	}
}

func TestExecute_RangeUpstream200PassesBodyToHandler(t *testing.T) {
	t.Parallel()

	proxyBody := []byte(`{"fileUrl":"http://example/file","fileName":"report.pdf"}`)
	fileGateway := &fileGatewayMock{res: out.FileResponse{StatusCode: 200, Headers: make(http.Header), Body: io.NopCloser(bytes.NewReader([]byte("partial-upstream")))}}

	svc := NewService(
		&proxyInfoGatewayMock{res: out.ProxyInfoResponse{StatusCode: 200, Body: proxyBody}},
		fileGateway,
		&archiveGatewayMock{},
		&conversionGatewayMock{},
	)

	res, err := svc.Execute(context.Background(), Request{RequestedID: "123", Range: "bytes=5-9"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	defer res.Body.Close()

	if fileGateway.callCount != 1 {
		t.Fatalf("expected one upstream call, got %d", fileGateway.callCount)
	}
	if got := fileGateway.allHeaders[0]["Range"]; got != "bytes=5-9" {
		t.Fatalf("expected call with range, got %q", got)
	}
	b, _ := io.ReadAll(res.Body)
	if string(b) != "partial-upstream" {
		t.Fatalf("unexpected body: %q", string(b))
	}
}

func TestExecute_RawRangeFallbackKeeps200ForHandlerSlicing(t *testing.T) {
	t.Parallel()

	proxyBody := []byte(`{"fileUrl":"http://example/file","fileName":"report.pdf"}`)
	fileGateway := &fileGatewayMock{res: out.FileResponse{
		StatusCode: http.StatusOK,
		Headers:    make(http.Header),
		Body:       io.NopCloser(bytes.NewReader([]byte("full-body"))),
	}}

	svc := NewService(
		&proxyInfoGatewayMock{res: out.ProxyInfoResponse{StatusCode: 200, Body: proxyBody}},
		fileGateway,
		&archiveGatewayMock{},
		&conversionGatewayMock{},
	)

	res, err := svc.Execute(context.Background(), Request{RequestedID: "123", Range: "bytes=999-1000"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.StatusCode)
	}
	if fileGateway.callCount != 1 {
		t.Fatalf("expected one upstream call, got %d", fileGateway.callCount)
	}
	if got := fileGateway.allHeaders[0]["Range"]; got != "bytes=999-1000" {
		t.Fatalf("expected call with range, got %q", got)
	}
	b, _ := io.ReadAll(res.Body)
	if string(b) != "full-body" {
		t.Fatalf("unexpected body: %q", string(b))
	}
}
