package computer

import (
	"context"
	"os"
)

func uploadBytes(ctx context.Context, sandbox RemoteComputer, data []byte, remotePath string) error {
	tmp, err := os.CreateTemp("", "amux-upload-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(tmp.Name())
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return sandbox.UploadFile(ctx, tmp.Name(), remotePath)
}

func downloadBytes(ctx context.Context, sandbox RemoteComputer, remotePath string) ([]byte, error) {
	tmp, err := os.CreateTemp("", "amux-download-*")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = os.Remove(tmp.Name())
	}()
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := sandbox.DownloadFile(ctx, remotePath, tmp.Name()); err != nil {
		return nil, err
	}
	return os.ReadFile(tmp.Name())
}
