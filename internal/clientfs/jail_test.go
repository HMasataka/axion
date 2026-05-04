package clientfs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func TestNew_RejectsNonexistentRoot(t *testing.T) {
	// Given: 存在しないパス
	nonexistent := filepath.Join(t.TempDir(), "does_not_exist")

	// When: Jail を作成する
	_, err := New(nonexistent)

	// Then: エラーが返る
	if err == nil {
		t.Fatal("expected error for nonexistent root, got nil")
	}
}

func TestNew_RejectsFileAsRoot(t *testing.T) {
	// Given: ファイル（ディレクトリではない）
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	// When: ファイルを root として Jail を作成する
	_, err := New(file)

	// Then: エラーが返る
	if err == nil {
		t.Fatal("expected error when root is a file, got nil")
	}
}

func TestResolve_NormalRelative(t *testing.T) {
	// Given: 有効な Jail
	root := t.TempDir()
	j, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	// When: 通常の相対パスを解決する
	got, err := j.Resolve("foo/bar.txt")

	// Then: root 配下の絶対パスが返る
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(j.Root(), "foo", "bar.txt")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestResolve_RejectsAbsolute(t *testing.T) {
	// Given: 有効な Jail
	root := t.TempDir()
	j, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	// When: 絶対パスを渡す
	_, err = j.Resolve("/etc/passwd")

	// Then: ErrPathEscape が返る
	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("expected ErrPathEscape, got %v", err)
	}
}

func TestResolve_RejectsParentEscape(t *testing.T) {
	// Given: 有効な Jail
	root := t.TempDir()
	j, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	// When: `..` で root を抜けようとする
	_, err = j.Resolve("../../etc/passwd")

	// Then: ErrPathEscape が返る
	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("expected ErrPathEscape, got %v", err)
	}
}

func TestResolve_RejectsParentTraversalInMiddle(t *testing.T) {
	// Given: 有効な Jail
	root := t.TempDir()
	j, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	// When: 途中に `..` が含まれ root を抜けるパスを渡す
	_, err = j.Resolve("foo/../../bar")

	// Then: ErrPathEscape が返る
	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("expected ErrPathEscape, got %v", err)
	}
}

func TestResolve_AllowsParentInBounds(t *testing.T) {
	// Given: 有効な Jail
	root := t.TempDir()
	j, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	// When: `..` があっても root 内に収まるパスを渡す
	got, err := j.Resolve("foo/../bar")

	// Then: root/bar が返る
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(j.Root(), "bar")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestResolve_RejectsSymlinkEscape(t *testing.T) {
	// Given: root 内に root 外を指す symlink がある
	root := t.TempDir()
	outside := t.TempDir()
	j, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	// When: symlink を通じて root 外へ出るパスを解決する
	_, err = j.Resolve("escape")

	// Then: ErrPathEscape が返る
	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("expected ErrPathEscape, got %v", err)
	}
}

func TestResolve_AllowsRootItself(t *testing.T) {
	// Given: 有効な Jail
	root := t.TempDir()
	j, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	// When: "." を解決する
	got, err := j.Resolve(".")

	// Then: root 自身が返る
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != j.Root() {
		t.Errorf("got %s, want %s", got, j.Root())
	}
}

func TestOpen_RejectsSymlink(t *testing.T) {
	// Given: 通常ファイルとその symlink が root 内にある
	root := t.TempDir()
	j, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "real.txt")
	if err := os.WriteFile(target, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	// When: symlink を Open する
	_, err = j.Open("link.txt")

	// Then: ErrUnsupportedFileType が返る
	if !errors.Is(err, ErrUnsupportedFileType) {
		t.Fatalf("expected ErrUnsupportedFileType, got %v", err)
	}
}

func TestOpen_NormalFile(t *testing.T) {
	// Given: root 内に通常ファイルがある
	root := t.TempDir()
	j, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("hello world")
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), want, 0644); err != nil {
		t.Fatal(err)
	}

	// When: 通常ファイルを Open する
	f, err := j.Open("hello.txt")

	// Then: 正常に開け、内容が一致する
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer f.Close()
	got := make([]byte, len(want))
	if _, err := f.Read(got); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStat_NormalFile(t *testing.T) {
	// Given: root 内に通常ファイルがある
	root := t.TempDir()
	j, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "stat.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	// When: Stat を呼ぶ
	info, err := j.Stat("stat.txt")

	// Then: FileInfo が返る
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Name() != "stat.txt" {
		t.Errorf("got name %s, want stat.txt", info.Name())
	}
}

func TestStat_RejectsSymlink(t *testing.T) {
	// Given: symlink が root 内にある
	root := t.TempDir()
	j, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "real.txt")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	// When: symlink に Stat を呼ぶ
	_, err = j.Stat("link.txt")

	// Then: ErrUnsupportedFileType が返る
	if !errors.Is(err, ErrUnsupportedFileType) {
		t.Fatalf("expected ErrUnsupportedFileType, got %v", err)
	}
}

func TestCreate_AndRead(t *testing.T) {
	// Given: 有効な Jail
	root := t.TempDir()
	j, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("created content")

	// When: Create でファイルを書き、Open で読む
	wf, err := j.Create("newfile.txt")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := wf.Write(want); err != nil {
		wf.Close()
		t.Fatal(err)
	}
	wf.Close()

	rf, err := j.Open("newfile.txt")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer rf.Close()
	got := make([]byte, len(want))
	if _, err := rf.Read(got); err != nil {
		t.Fatal(err)
	}

	// Then: 内容が一致する
	if string(got) != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCreate_AutoCreatesParentDirs(t *testing.T) {
	// Given: 有効な Jail（親ディレクトリが存在しない）
	root := t.TempDir()
	j, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	// When: 親ディレクトリが存在しないパスに Create する
	f, err := j.Create("a/b/c/file.txt")

	// Then: エラーなく作成される
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	f.Close()
	if _, err := os.Stat(filepath.Join(root, "a", "b", "c", "file.txt")); err != nil {
		t.Errorf("file not found after Create: %v", err)
	}
}

func TestRename_Success(t *testing.T) {
	// Given: root 内にファイルがある
	root := t.TempDir()
	j, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "old.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	// When: Rename する
	err = j.Rename("old.txt", "new.txt")

	// Then: 正常に rename される
	if err != nil {
		t.Fatalf("Rename failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "old.txt")); !os.IsNotExist(err) {
		t.Error("old.txt should not exist")
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); err != nil {
		t.Errorf("new.txt should exist: %v", err)
	}
}

func TestRename_RejectsEscape(t *testing.T) {
	// Given: root 内にファイルがある
	root := t.TempDir()
	j, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	// When: 移動先が root 外
	err = j.Rename("file.txt", "../../outside.txt")

	// Then: ErrPathEscape が返る
	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("expected ErrPathEscape, got %v", err)
	}
}

func TestRemove_Success(t *testing.T) {
	// Given: root 内にファイルがある
	root := t.TempDir()
	j, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "todelete.txt")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	// When: Remove する
	err = j.Remove("todelete.txt")

	// Then: ファイルが削除される
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("expected file to be removed")
	}
}

func TestReadDir_ListsEntries(t *testing.T) {
	// Given: root 内にファイルが複数ある
	root := t.TempDir()
	j, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// When: ReadDir を呼ぶ
	entries, err := j.ReadDir(".")

	// Then: 2エントリが返る
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("got %d entries, want 2", len(entries))
	}
}

func TestMkdirAll_Success(t *testing.T) {
	// Given: 有効な Jail
	root := t.TempDir()
	j, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	// When: MkdirAll でネストされたディレクトリを作る
	err = j.MkdirAll("x/y/z")

	// Then: ディレクトリが作成される
	if err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "x", "y", "z"))
	if err != nil {
		t.Fatalf("directory not found: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestStat_RejectsFIFO(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFO not supported on Windows")
	}

	// Given: root 内に FIFO がある
	root := t.TempDir()
	j, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatalf("Mkfifo failed: %v", err)
	}

	// When: FIFO に Stat を呼ぶ
	_, err = j.Stat("pipe")

	// Then: ErrUnsupportedFileType が返る
	if !errors.Is(err, ErrUnsupportedFileType) {
		t.Fatalf("expected ErrUnsupportedFileType, got %v", err)
	}
}
