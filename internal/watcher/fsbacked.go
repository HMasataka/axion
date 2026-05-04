package watcher

import (
	"io"
	"os"
)

// fsBackedFS は os パッケージ直接呼び出しを集約するデフォルト実装。
// forbidigo の対象を本ファイルに限定するために分離している。
type fsBackedFS struct{}

func (fsBackedFS) Open(rel string) (io.ReadCloser, error) {
	return os.Open(rel) //nolint:forbidigo
}

func (fsBackedFS) Stat(rel string) (os.FileInfo, error) {
	return os.Stat(rel) //nolint:forbidigo
}
