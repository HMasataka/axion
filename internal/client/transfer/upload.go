package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
)

// UploadResult は Upload の戻り値。
type UploadResult struct {
	SHA256  string
	Size    int64
	Skipped bool
}

// Upload は Jail 経由で rel ファイルを開き、SHA256 を計算しつつサーバーへ PUT する。
// サーバーが既に同 sha を持つ場合 (HEAD で 200) は skip して Skipped=true で返す。
func (c *Client) Upload(ctx context.Context, rel string) (UploadResult, error) {
	sha, size, err := c.computeSHA(rel)
	if err != nil {
		return UploadResult{}, fmt.Errorf("transfer: compute sha: %w", err)
	}

	exists, err := c.headBlob(ctx, sha)
	if err != nil {
		return UploadResult{}, fmt.Errorf("transfer: head blob: %w", err)
	}
	if exists {
		return UploadResult{SHA256: sha, Size: size, Skipped: true}, nil
	}

	f, err := c.cfg.Jail.Open(rel)
	if err != nil {
		return UploadResult{}, fmt.Errorf("transfer: open for put: %w", err)
	}
	defer f.Close()

	if err := c.putBlob(ctx, sha, size, f); err != nil {
		return UploadResult{}, err
	}

	return UploadResult{SHA256: sha, Size: size, Skipped: false}, nil
}

func (c *Client) computeSHA(rel string) (string, int64, error) {
	f, err := c.cfg.Jail.Open(rel)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func (c *Client) headBlob(ctx context.Context, sha string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, c.blobURL(sha), nil)
	if err != nil {
		return false, err
	}
	c.setAuthHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("HEAD blob: unexpected status %d", resp.StatusCode)
	}
}

func (c *Client) putBlob(ctx context.Context, sha string, size int64, body io.Reader) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.blobURL(sha), body)
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	c.setAuthHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("PUT blob: status %d, body=%s", resp.StatusCode, errBody)
	}
	return nil
}
