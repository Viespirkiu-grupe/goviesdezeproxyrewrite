package http

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	archiveapp "github.com/Viespirkiu-grupe/goviesdezeproxyrewrite/internal/app/archive"
	"github.com/go-chi/chi/v5"
)

var idRe = regexp.MustCompile(`^\d+$|^[a-fA-F0-9]{32}$`)

type Handler struct {
	service *archiveapp.Service
}

func NewHandler(service *archiveapp.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) HandleArchive(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	dokID := chi.URLParam(r, "dokId")
	fileID := chi.URLParam(r, "fileId")
	pathFile := chi.URLParam(r, "*")

	requestedID, err := resolveRequestedID(id, dokID, fileID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := h.service.Execute(r.Context(), archiveapp.Request{
		RequestedID: requestedID,
		PathFile:    pathFile,
		ConvertTo:   r.URL.Query().Get("convertTo"),
		Index:       r.URL.Query().Get("index"),
		Range:       r.Header.Get("Range"),
		IfRange:     r.Header.Get("If-Range"),
	})
	if err != nil {
		if errors.Is(err, r.Context().Err()) {
			return
		}
		var statusErr *archiveapp.StatusError
		if errors.As(err, &statusErr) {
			http.Error(w, statusErr.Message, statusErr.Status)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer result.Body.Close()

	if result.ContentType != "" {
		w.Header().Set("Content-Type", result.ContentType)
	}
	if result.FileName != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename*=UTF-8''%s", url.PathEscape(path.Base(result.FileName))))
	}
	if result.CacheControl != "" {
		w.Header().Set("Cache-Control", result.CacheControl)
	}
	if result.ForwardAcceptRange != "" {
		w.Header().Set("Accept-Ranges", result.ForwardAcceptRange)
	}
	if result.ForwardContentRange != "" {
		w.Header().Set("Content-Range", result.ForwardContentRange)
	}
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		w.WriteHeader(result.StatusCode)
		_, _ = io.Copy(w, result.Body)
		return
	}

	if r.Header.Get("Range") != "" {
		h.handleRangeResponse(w, r, result)
		return
	}

	w.WriteHeader(result.StatusCode)
	_, _ = io.Copy(w, result.Body)
}

func (h *Handler) handleRangeResponse(w http.ResponseWriter, r *http.Request, result *archiveapp.Result) {
	rng, ok := parseClosedRange(r.Header.Get("Range"))
	if (result.StatusCode == http.StatusPartialContent || result.StatusCode == http.StatusOK) && ok {
		expectedLen := rng.end - rng.start + 1
		if expectedLen > 0 {
			limited := io.LimitReader(result.Body, expectedLen+1)
			chunk, err := io.ReadAll(limited)
			if err != nil {
				http.Error(w, "failed to read ranged upstream body", http.StatusBadGateway)
				return
			}
			if int64(len(chunk)) == expectedLen {
				if w.Header().Get("Content-Range") == "" {
					w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/*", rng.start, rng.end))
				}
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(chunk)
				_, _ = io.Copy(io.Discard, result.Body)
				return
			}
			if int64(len(chunk)) > expectedLen {
				rest, readErr := io.ReadAll(result.Body)
				if readErr != nil {
					http.Error(w, "failed to read full upstream body", http.StatusBadGateway)
					return
				}
				fullBody := make([]byte, 0, len(chunk)+len(rest))
				fullBody = append(fullBody, chunk...)
				fullBody = append(fullBody, rest...)
				w.Header().Del("Content-Range")
				http.ServeContent(w, r, path.Base(result.FileName), time.Time{}, bytes.NewReader(fullBody))
				return
			}
			http.Error(w, "requested range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
			return
		}
	}

	body, readErr := io.ReadAll(result.Body)
	if readErr != nil {
		http.Error(w, "failed to read response body", http.StatusBadGateway)
		return
	}
	http.ServeContent(w, r, path.Base(result.FileName), time.Time{}, bytes.NewReader(body))
}

type closedRange struct {
	start int64
	end   int64
}

func parseClosedRange(v string) (closedRange, bool) {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "bytes=") {
		return closedRange{}, false
	}
	parts := strings.Split(strings.TrimPrefix(v, "bytes="), ",")
	if len(parts) != 1 {
		return closedRange{}, false
	}
	se := strings.Split(strings.TrimSpace(parts[0]), "-")
	if len(se) != 2 || se[0] == "" || se[1] == "" {
		return closedRange{}, false
	}
	start, err := strconv.ParseInt(se[0], 10, 64)
	if err != nil {
		return closedRange{}, false
	}
	end, err := strconv.ParseInt(se[1], 10, 64)
	if err != nil {
		return closedRange{}, false
	}
	if start < 0 || end < start {
		return closedRange{}, false
	}
	return closedRange{start: start, end: end}, true
}

func resolveRequestedID(id, dokID, fileID string) (string, error) {
	switch {
	case dokID != "" && fileID != "":
		if _, err := strconv.Atoi(dokID); err != nil {
			return "", errors.New("dokId must be a number")
		}
		if _, err := strconv.Atoi(fileID); err != nil {
			return "", errors.New("fileId must be a number")
		}
		return dokID + "/" + fileID, nil
	case id != "":
		if !idRe.MatchString(id) {
			return "", errors.New("id must be a number or MD5")
		}
		return id, nil
	default:
		return "", errors.New("id not provided")
	}
}
