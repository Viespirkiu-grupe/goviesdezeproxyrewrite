package domain

// ProxyInfo is the upstream payload describing how to fetch/serve the file.
type ProxyInfo struct {
	FileURL            string            `json:"fileUrl"`
	Extension          string            `json:"extension"`
	ContainerExtension string            `json:"containerExtension"`
	Extract            string            `json:"extract"`
	Headers            map[string]string `json:"headers"`
	ContentType        string            `json:"contentType"`
	ContentLength      int               `json:"contentLength"`
	FileName           string            `json:"fileName"`
}
