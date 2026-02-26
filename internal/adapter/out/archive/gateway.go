package archive

import (
	"io"

	"github.com/Viespirkiu-grupe/goviesdezeproxyrewrite/ziputil"
)

type Gateway struct{}

func NewGateway() *Gateway {
	return &Gateway{}
}

func (g *Gateway) ListFiles(archiveBytes []byte) ([]string, error) {
	return ziputil.IdentityFilesV2(archiveBytes)
}

func (g *Gateway) ExtractFile(archiveBytes []byte, filename string) (io.ReadCloser, error) {
	return ziputil.GetFileFromArchiveV2(archiveBytes, filename)
}

func (g *Gateway) ExtractEmlAttachment(in []byte, filename, idx string) (io.ReadCloser, error) {
	return ziputil.ExtractEmlAttachments(in, filename, idx)
}

func (g *Gateway) ConvertMsgToEml(in []byte) ([]byte, error) {
	return ziputil.ConvertMsgToEml(in)
}
