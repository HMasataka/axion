package clientfs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// unsupportedModeMask は通常ファイル/ディレクトリ以外の種別を検出するマスク。
// symlink 自体はここで弾き、EvalSymlinks で解決済みの先は通す設計。
const unsupportedModeMask = os.ModeSymlink |
	os.ModeDevice |
	os.ModeNamedPipe |
	os.ModeSocket |
	os.ModeCharDevice |
	os.ModeIrregular

// Jail はクライアント側のファイルアクセスを root ディレクトリ内に制限する。
type Jail struct {
	root string // EvalSymlinks 解決済みの絶対パス
}

// New は root を絶対パス化＋シンボリックリンク解決した Jail を返す。
// root が存在しない、ディレクトリでない場合はエラー。
func New(root string) (*Jail, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("clientfs: abs failed: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("clientfs: root does not exist: %w", err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return nil, fmt.Errorf("clientfs: stat root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("clientfs: root is not a directory: %s", real)
	}
	return &Jail{root: real}, nil
}

// Root は jail の root 絶対パスを返す。
func (j *Jail) Root() string {
	return j.root
}

// Resolve は rel パスを root 配下の絶対パスへ解決する。
// 返り値の path は root 配下の絶対パス。
func (j *Jail) Resolve(rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", ErrPathEscape
	}

	cleaned := filepath.Clean(filepath.Join(j.root, rel))

	real, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		// 未作成ファイルは既存の祖先まで再帰的に遡って解決する
		real, err = resolveNonExistent(cleaned)
		if err != nil {
			return "", fmt.Errorf("clientfs: resolve failed: %w", err)
		}
	}

	return j.checkUnderRoot(real)
}

// resolveNonExistent は cleaned が存在しないとき、存在する祖先まで遡って
// EvalSymlinks を試みた後 basename を付け直すことで安全な絶対パスを返す。
func resolveNonExistent(cleaned string) (string, error) {
	// ファイルシステムのルートまで来てしまったら諦める
	parent := filepath.Dir(cleaned)
	if parent == cleaned {
		return "", fmt.Errorf("no existing ancestor found for %s", cleaned)
	}

	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		// 親も存在しないなら再帰する
		realParent, err = resolveNonExistent(parent)
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(realParent, filepath.Base(cleaned)), nil
}

// resolveChecked は Resolve を行う前に cleaned パスを Lstat して
// symlink/FIFO 等の非サポート種別を弾く。Open/Stat 専用。
func (j *Jail) resolveChecked(rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", ErrPathEscape
	}
	cleaned := filepath.Clean(filepath.Join(j.root, rel))

	// Lstat で symlink 自体を検出する（EvalSymlinks で解決する前に判定）
	info, err := os.Lstat(cleaned)
	if err == nil && info.Mode()&unsupportedModeMask != 0 {
		return "", ErrUnsupportedFileType
	}

	return j.Resolve(rel)
}

// checkUnderRoot は real が root 配下かを検証して real を返す。
func (j *Jail) checkUnderRoot(real string) (string, error) {
	rel, err := filepath.Rel(j.root, real)
	if err != nil {
		return "", ErrPathEscape
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrPathEscape
	}
	return real, nil
}

// Open は rel を読み取りモードで開く。通常ファイルのみ許可。
func (j *Jail) Open(rel string) (*os.File, error) {
	path, err := j.resolveChecked(rel)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

// Create は rel を作成モードで開く（O_CREATE|O_TRUNC|O_WRONLY、0644）。
// 親ディレクトリが存在しなければ MkdirAll(0755) で作成する。
func (j *Jail) Create(rel string) (*os.File, error) {
	path, err := j.Resolve(rel)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("clientfs: mkdirall: %w", err)
	}
	return os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
}

// Stat は rel の os.FileInfo を返す。symlink は Lstat で弾く。
func (j *Jail) Stat(rel string) (os.FileInfo, error) {
	path, err := j.resolveChecked(rel)
	if err != nil {
		return nil, err
	}
	return os.Stat(path)
}

// ReadDir は rel ディレクトリのエントリを返す。
func (j *Jail) ReadDir(rel string) ([]os.DirEntry, error) {
	path, err := j.Resolve(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadDir(path)
}

// Rename は old → new に rename する。両方 jail 内であること。
func (j *Jail) Rename(oldRel, newRel string) error {
	oldPath, err := j.Resolve(oldRel)
	if err != nil {
		return err
	}
	newPath, err := j.Resolve(newRel)
	if err != nil {
		return err
	}
	return os.Rename(oldPath, newPath)
}

// Remove は rel を削除する。
func (j *Jail) Remove(rel string) error {
	path, err := j.Resolve(rel)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// MkdirAll は rel ディレクトリを再帰的に作成する（perm 0755 固定）。
func (j *Jail) MkdirAll(rel string) error {
	path, err := j.Resolve(rel)
	if err != nil {
		return err
	}
	return os.MkdirAll(path, 0755)
}
