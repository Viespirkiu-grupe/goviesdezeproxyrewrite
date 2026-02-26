package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
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
	infoRes, err := s.proxyInfo.FetchProxyInfo(ctx, req.RequestedID)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		return nil, &StatusError{Status: 502, Message: "failed to fetch proxy info"}
	}

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

	fileRes, err := s.file.FetchFile(ctx, info.FileURL, info.Headers)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		return nil, &StatusError{Status: 502, Message: "failed to fetch file"}
	}

	if fileRes.StatusCode < 200 || fileRes.StatusCode >= 300 {
		return &Result{
			StatusCode: fileRes.StatusCode,
			Body:       fileRes.Body,
		}, nil
	}

	body := fileRes.Body
	name := info.FileName
	target := info.Extract
	if target == "" {
		target = req.PathFile
	}
	target = strings.TrimSpace(target)
	target = strings.TrimPrefix(target, "/")
	target = path.Clean(target)
	if target == "." {
		target = ""
	}

	if target != "" {
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
	convertTo := strings.ToLower(req.ConvertTo)
	if convertTo != "" {
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
		ForwardAcceptRange:  fileRes.Headers.Get("Accept-Ranges"),
		ForwardContentRange: fileRes.Headers.Get("Content-Range"),
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
	case "jpg", "jpeg", "png", "tif", "tiff", "bmp", "prn", "gif", "jfif", "heic":
		return true
	default:
		return false
	}
}

func isImageExt(ext string) bool {
	return isImageTarget(ext)
}
