package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrSHAMismatch はダウンロードした blob の SHA256 が期待値と一致しないときに返す。
var ErrSHAMismatch = errors.New("transfer: downloaded blob sha mismatch")

// Download は GET /v1/blobs/{sha} を取得し、Jail 経由で rel に書く。
// `<rel>.partial` を経由して atomic rename する。
// partial が存在する場合は削除して fresh ダウンロードとする。
func (c *Client) Download(ctx context.Context, rel, sha string) error {
	partial := rel + ".partial"

	// 既存 partial は削除して fresh ダウンロード
	if _, err := c.cfg.Jail.Stat(partial); err == nil {
		if err := c.cfg.Jail.Remove(partial); err != nil {
			return fmt.Errorf("transfer: remove partial: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.blobURL(sha), nil)
	if err != nil {
		return err
	}
	c.setAuthHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusPartialContent:
		// 続行
	case http.StatusNotFound:
		return fmt.Errorf("blob not found: %s", sha)
	default:
		return fmt.Errorf("GET blob: status %d", resp.StatusCode)
	}

	wf, err := c.cfg.Jail.Create(partial)
	if err != nil {
		return fmt.Errorf("transfer: create partial: %w", err)
	}

	hasher := sha256.New()
	mw := io.MultiWriter(wf, hasher)
	if _, err := io.Copy(mw, resp.Body); err != nil {
		wf.Close()
		return fmt.Errorf("transfer: copy body: %w", err)
	}
	if err := wf.Close(); err != nil {
		return fmt.Errorf("transfer: close partial: %w", err)
	}

	gotSHA := hex.EncodeToString(hasher.Sum(nil))
	if gotSHA != sha {
		return ErrSHAMismatch
	}

	if err := c.cfg.Jail.Rename(partial, rel); err != nil {
		return fmt.Errorf("transfer: rename partial: %w", err)
	}
	return nil
}
