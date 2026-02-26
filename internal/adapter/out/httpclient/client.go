package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Viespirkiu-grupe/goviesdezeproxyrewrite/internal/port/out"
)

type Client struct {
	baseURL    *url.URL
	apiKey     string
	httpClient *http.Client
}

func New(baseURL *url.URL, apiKey string, httpClient *http.Client) *Client {
	return &Client{baseURL: baseURL, apiKey: apiKey, httpClient: httpClient}
}

func (c *Client) FetchProxyInfo(ctx context.Context, requestedID string) (out.ProxyInfoResponse, error) {
	infoURL := *c.baseURL
	infoURL.Path = strings.TrimRight(c.baseURL.Path, "/") + "/failas/" + requestedID + "/downloadProxyInformation"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, infoURL.String(), nil)
	if err != nil {
		return out.ProxyInfoResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return out.ProxyInfoResponse{}, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return out.ProxyInfoResponse{}, err
	}

	return out.ProxyInfoResponse{StatusCode: res.StatusCode, Body: body}, nil
}

func (c *Client) FetchFile(ctx context.Context, fileURL string, headers map[string]string) (out.FileResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return out.FileResponse{}, err
	}

	for k, v := range headers {
		switch strings.ToLower(k) {
		case "connection", "proxy-connection", "keep-alive", "transfer-encoding", "upgrade", "te", "trailer":
			continue
		default:
			req.Header.Set(k, v)
		}
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return out.FileResponse{}, err
	}

	return out.FileResponse{
		StatusCode: res.StatusCode,
		Headers:    res.Header.Clone(),
		Body:       res.Body,
	}, nil
}
