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
	res out.FileResponse
	err error
}

func (m *fileGatewayMock) FetchFile(_ context.Context, _ string, _ map[string]string) (out.FileResponse, error) {
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
