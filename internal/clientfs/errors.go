package clientfs

import "errors"

var (
	// ErrPathEscape は rel パスが root の外を指しているときに返される。
	ErrPathEscape = errors.New("clientfs: path escapes jail root")

	// ErrUnsupportedFileType は symlink/FIFO/socket/device など
	// 通常ファイル/ディレクトリ以外のエントリにアクセスしようとしたときに返される。
	ErrUnsupportedFileType = errors.New("clientfs: unsupported file type")
)
