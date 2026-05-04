package id

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// LoadOrCreate は idFile から既存 client.id を読む。無ければ新しい UUIDv4 を生成して保存する。
// 親ディレクトリが無ければ MkdirAll(0700) で作成。ファイルパーミッションは 0600。
func LoadOrCreate(idFile string) (string, error) {
	data, err := os.ReadFile(idFile) //nolint:forbidigo // client.id は jail 対象外
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("id: read file: %w", err)
	}

	if err == nil {
		id := strings.TrimSpace(string(data))
		if _, parseErr := uuid.Parse(id); parseErr != nil {
			return "", fmt.Errorf("id: invalid uuid in %s: %w", idFile, parseErr)
		}
		return id, nil
	}

	newID := uuid.New().String()

	if err := os.MkdirAll(filepath.Dir(idFile), 0700); err != nil { //nolint:forbidigo // client config dir setup
		return "", fmt.Errorf("id: mkdirall: %w", err)
	}

	if err := os.WriteFile(idFile, []byte(newID), 0600); err != nil { //nolint:forbidigo // client.id は jail 対象外
		return "", fmt.Errorf("id: write file: %w", err)
	}

	return newID, nil
}
