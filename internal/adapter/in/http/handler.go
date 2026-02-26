package http

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"

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

	w.WriteHeader(result.StatusCode)
	_, _ = io.Copy(w, result.Body)
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
