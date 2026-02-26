package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/Viespirkiu-grupe/goviesdezeproxyrewrite/internal/domain"
	"github.com/Viespirkiu-grupe/goviesdezeproxyrewrite/internal/port/out"
	"github.com/Viespirkiu-grupe/goviesdezeproxyrewrite/utils"
)

type Request struct {
	RequestedID string
	PathFile    string
	ConvertTo   string
	Index       string
	Range       string
	IfRange     string
}

type Result struct {
	StatusCode          int
	Body                io.ReadCloser
	ContentType         string
	FileName            string
	CacheControl        string
	ForwardAcceptRange  string
	ForwardContentRange string
}

type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string {
	return e.Message
}

type Service struct {
	proxyInfo out.ProxyInfoGateway
	file      out.FileGateway
	archive   out.ArchiveGateway
	convert   out.ConversionGateway
}

func NewService(
	proxyInfo out.ProxyInfoGateway,
	file out.FileGateway,
	archive out.ArchiveGateway,
	convert out.ConversionGateway,
) *Service {
	return &Service{
		proxyInfo: proxyInfo,
		file:      file,
		archive:   archive,
		convert:   convert,
	}
}

func (s *Service) Execute(ctx context.Context, req Request) (*Result, error) {
	log.Printf("archive: fetch proxy info requestedID=%s", req.RequestedID)
	infoRes, err := s.proxyInfo.FetchProxyInfo(ctx, req.RequestedID)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		return nil, &StatusError{Status: 502, Message: "failed to fetch proxy info"}
	}
	log.Printf("archive: proxy info status=%d requestedID=%s", infoRes.StatusCode, req.RequestedID)

	if infoRes.StatusCode < 200 || infoRes.StatusCode >= 300 {
		return &Result{
			StatusCode: infoRes.StatusCode,
			Body:       io.NopCloser(bytes.NewReader(infoRes.Body)),
		}, nil
	}

	var info domain.ProxyInfo
	if err := json.Unmarshal(infoRes.Body, &info); err != nil {
		return nil, &StatusError{Status: 502, Message: "invalid proxy info json"}
	}

	if info.FileURL == "" {
		return nil, &StatusError{Status: 502, Message: "proxy info missing fileUrl"}
	}

	convertTo := strings.ToLower(strings.TrimSpace(req.ConvertTo))
	target := info.Extract
	if target == "" {
		target = req.PathFile
	}
	target = strings.TrimSpace(target)
	target = strings.TrimPrefix(target, "/")
	target = path.Clean(target)
	if target == "0" {
		target = ""
	}
	if target == "." {
		target = ""
	}

	requiresExtraction := target != ""
	requiresConversion := convertTo != ""
	forwardRangeToUpstream := req.Range != "" && !requiresExtraction && !requiresConversion

	log.Printf("archive: fetch file requestedID=%s", req.RequestedID)
	upstreamHeaders := cloneHeaders(info.Headers)
	if forwardRangeToUpstream {
		upstreamHeaders["Range"] = req.Range
		if req.IfRange != "" {
			upstreamHeaders["If-Range"] = req.IfRange
		}
	}

	fileRes, err := s.file.FetchFile(ctx, info.FileURL, upstreamHeaders)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		return nil, &StatusError{Status: 502, Message: "failed to fetch file"}
	}
	log.Printf("archive: file status=%d requestedID=%s", fileRes.StatusCode, req.RequestedID)

	if forwardRangeToUpstream && fileRes.StatusCode == http.StatusOK {
		_ = fileRes.Body.Close()
		log.Printf("archive: upstream ignored range, refetching full body requestedID=%s", req.RequestedID)
		fileRes, err = s.file.FetchFile(ctx, info.FileURL, cloneHeaders(info.Headers))
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, err
			}
			return nil, &StatusError{Status: 502, Message: "failed to refetch full file"}
		}
		log.Printf("archive: refetch file status=%d requestedID=%s", fileRes.StatusCode, req.RequestedID)
	}

	if fileRes.StatusCode < 200 || fileRes.StatusCode >= 300 {
		return &Result{
			StatusCode: fileRes.StatusCode,
			Body:       fileRes.Body,
		}, nil
	}

	body := fileRes.Body
	name := info.FileName

	if target != "" {
		log.Printf("archive: extract target=%q requestedID=%s", target, req.RequestedID)
		archiveBytes, err := io.ReadAll(fileRes.Body)
		_ = fileRes.Body.Close()
		if err != nil {
			return nil, &StatusError{Status: 502, Message: "error reading upstream body"}
		}

		ext := info.ContainerExtension
		if ext == "" {
			ext = info.Extension
		}
		ext = strings.ToLower(ext)

		switch ext {
		case "eml":
			body, err = s.archive.ExtractEmlAttachment(archiveBytes, target, req.Index)
			if err != nil {
				return nil, &StatusError{Status: 502, Message: "error extracting eml attachment"}
			}
			name = target
		case "msg":
			eml, err := s.archive.ConvertMsgToEml(archiveBytes)
			if err != nil {
				return nil, &StatusError{Status: 502, Message: "error converting msg"}
			}
			body, err = s.archive.ExtractEmlAttachment(eml, target, req.Index)
			if err != nil {
				return nil, &StatusError{Status: 502, Message: "error extracting msg attachment"}
			}
			name = target
		default:
			files, err := s.archive.ListFiles(archiveBytes)
			if err != nil {
				log.Printf("failed to open archive: %v", err)
				return nil, &StatusError{Status: 502, Message: "invalid archive"}
			}

			best, err := bestMatch(target, files)
			if err != nil {
				return nil, &StatusError{Status: 404, Message: "file not found in archive"}
			}

			body, err = s.archive.ExtractFile(archiveBytes, best)
			if err != nil {
				return nil, &StatusError{Status: 502, Message: "error extracting file"}
			}
			name = best
		}
	}

	if info.Extract != "" {
		name = info.FileName
	}

	contentType := contentTypeFromExt(name)
	if convertTo != "" {
		if !isSupportedConvertTo(convertTo) {
			_ = body.Close()
			return nil, &StatusError{Status: 400, Message: "unsupported convertTo value"}
		}

		if isImageTarget(convertTo) && !isImageExt(strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")) {
			_ = body.Close()
			return nil, &StatusError{Status: 400, Message: "source file is not an image"}
		}

		if convertTo == "pdf" || isImageTarget(convertTo) {
			converted, convertedName, convertedType, err := s.convert.Convert(ctx, body, name, convertTo)
			if err != nil {
				_ = body.Close()
				return nil, &StatusError{Status: 500, Message: "conversion failed"}
			}
			if converted != body {
				_ = body.Close()
			}
			body = converted
			name = convertedName
			contentType = convertedType
		}
	}

	return &Result{
		StatusCode:          fileRes.StatusCode,
		Body:                body,
		ContentType:         contentType,
		FileName:            name,
		CacheControl:        "public, max-age=2592000, immutable",
		ForwardAcceptRange:  forwardedAcceptRanges(fileRes.StatusCode, fileRes.Headers.Get("Accept-Ranges")),
		ForwardContentRange: forwardedContentRange(fileRes.StatusCode, fileRes.Headers.Get("Content-Range")),
	}, nil
}

func bestMatch(file string, files []string) (string, error) {
	if file == "" {
		return "", errors.New("file not found in archive")
	}

	normTarget := normalizePath(file)
	baseTarget := strings.ToLower(path.Base(normTarget))

	for _, f := range files {
		if strings.EqualFold(normalizePath(f), normTarget) {
			return f, nil
		}
	}

	for _, f := range files {
		if strings.EqualFold(strings.ToLower(path.Base(normalizePath(f))), baseTarget) {
			return f, nil
		}
	}

	var best string
	bestSim := 0.0
	for _, f := range files {
		fullSim := utils.Similarity(normalizePath(f), normTarget)
		baseSim := utils.Similarity(strings.ToLower(path.Base(normalizePath(f))), baseTarget)
		sim := fullSim
		if baseSim > sim {
			sim = baseSim
		}
		if sim > bestSim || strings.EqualFold(f, file) {
			bestSim = sim
			best = f
		}
	}
	if bestSim < 0.3 {
		return "", errors.New("file not found in archive")
	}
	return best, nil
}

func normalizePath(v string) string {
	v = strings.ReplaceAll(v, "\\", "/")
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "./")
	v = strings.TrimPrefix(v, "/")
	v = path.Clean(v)
	if v == "." {
		return ""
	}
	return v
}

func contentTypeFromExt(filename string) string {
	ext := filepath.Ext(filename)
	if ext == "" {
		return "application/octet-stream"
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

func isImageTarget(v string) bool {
	switch v {
	case "jpg", "jpeg", "png", "tif", "tiff", "bmp", "prn", "gif", "jfif", "heic", "webp":
		return true
	default:
		return false
	}
}

func isImageExt(ext string) bool {
	return isImageTarget(ext)
}

func isSupportedConvertTo(v string) bool {
	if v == "pdf" {
		return true
	}
	return isImageTarget(v)
}

func cloneHeaders(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func forwardedAcceptRanges(status int, value string) string {
	if status == http.StatusPartialContent || status == http.StatusRequestedRangeNotSatisfiable {
		return value
	}
	return ""
}

func forwardedContentRange(status int, value string) string {
	if status == http.StatusPartialContent || status == http.StatusRequestedRangeNotSatisfiable {
		return value
	}
	return ""
}
