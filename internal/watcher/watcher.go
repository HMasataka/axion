package watcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const defaultDebounceDelay = 200 * time.Millisecond

// Opener はファイルを読み取りモードで開く責務。
// jail 経由のファイルアクセスを許容するため interface 化。
type Opener interface {
	Open(rel string) (io.ReadCloser, error)
}

// Stater はファイルメタ情報を返す責務。
type Stater interface {
	Stat(rel string) (os.FileInfo, error)
}

// Config は Watcher の設定。
type Config struct {
	Root          string
	IgnoreList    []string
	DebounceDelay time.Duration
	Opener        Opener
	Stater        Stater
}

// Event は変更通知。
type Event struct {
	RelPath string
	Op      Op
	SHA256  string
	Size    int64
	ModTime time.Time
}

type Op int

const (
	OpWrite Op = iota
	OpRemove
)

type Watcher struct {
	cfg       Config
	fsWatcher *fsnotify.Watcher
	events    chan Event
	errors    chan error
	cancel    context.CancelFunc
	mu        sync.RWMutex
	fileHashes map[string]string
}

func New(cfg Config) (*Watcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if cfg.DebounceDelay == 0 {
		cfg.DebounceDelay = defaultDebounceDelay
	}
	if cfg.Opener == nil {
		cfg.Opener = fsBackedFS{}
	}
	if cfg.Stater == nil {
		cfg.Stater = fsBackedFS{}
	}

	return &Watcher{
		cfg:        cfg,
		fsWatcher:  fsWatcher,
		events:     make(chan Event, 100),
		errors:     make(chan error, 10),
		fileHashes: make(map[string]string),
	}, nil
}

func (w *Watcher) Start(ctx context.Context) error {
	if err := w.addDirRecursive(w.cfg.Root); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel

	go w.watchLoop(ctx)
	return nil
}

func (w *Watcher) Events() <-chan Event {
	return w.events
}

func (w *Watcher) Errors() <-chan error {
	return w.errors
}

func (w *Watcher) Stop() error {
	if w.cancel != nil {
		w.cancel()
	}
	return w.fsWatcher.Close()
}

func (w *Watcher) addDirRecursive(path string) error {
	return filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if w.shouldIgnore(p) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			return w.fsWatcher.Add(p)
		}

		return nil
	})
}

func (w *Watcher) shouldIgnore(path string) bool {
	relPath, err := filepath.Rel(w.cfg.Root, path)
	if err != nil {
		return false
	}
	return MatchIgnore(relPath, w.cfg.IgnoreList)
}

// MatchIgnore は relPath が patterns のいずれかに該当すれば true を返す。
// relPath は root からの相対パス。マッチ規則は:
//   - filepath.Match(pattern, basename(relPath))
//   - strings.HasPrefix(relPath, pattern)
func MatchIgnore(relPath string, patterns []string) bool {
	base := filepath.Base(relPath)
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(pattern, base); matched {
			return true
		}
		if strings.HasPrefix(relPath, pattern) {
			return true
		}
	}
	return false
}

// pendingEvent は debounce 中の同 path への Op フラグ蓄積用。
// macOS は Create/Write の直後に Chmod を発行することがあり、Chmod 単独では
// processEvent でハンドルされない。Op を OR 集約することで Write/Create を保持する。
type pendingEvent struct {
	op    fsnotify.Op
	timer *time.Timer
}

func (w *Watcher) watchLoop(ctx context.Context) {
	debouncer := make(map[string]*pendingEvent)
	var debounceMu sync.Mutex

	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
			select {
			case w.errors <- err:
			default:
			}
		case fsEvent, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}

			if w.shouldIgnore(fsEvent.Name) {
				continue
			}

			name := fsEvent.Name
			debounceMu.Lock()
			p, exists := debouncer[name]
			if exists {
				p.op |= fsEvent.Op
				p.timer.Stop()
			} else {
				p = &pendingEvent{op: fsEvent.Op}
				debouncer[name] = p
			}
			p.timer = time.AfterFunc(w.cfg.DebounceDelay, func() {
				debounceMu.Lock()
				e, ok := debouncer[name]
				if !ok {
					debounceMu.Unlock()
					return
				}
				op := e.op
				delete(debouncer, name)
				debounceMu.Unlock()
				w.processEvent(fsnotify.Event{Name: name, Op: op})
			})
			debounceMu.Unlock()
		}
	}
}

func (w *Watcher) processEvent(fsEvent fsnotify.Event) {
	relPath, err := filepath.Rel(w.cfg.Root, fsEvent.Name)
	if err != nil {
		return
	}

	relPath = filepath.ToSlash(relPath)

	switch {
	case fsEvent.Op&fsnotify.Write == fsnotify.Write,
		fsEvent.Op&fsnotify.Create == fsnotify.Create:
		w.handleWrite(fsEvent.Name, relPath)

	case fsEvent.Op&fsnotify.Remove == fsnotify.Remove,
		fsEvent.Op&fsnotify.Rename == fsnotify.Rename:
		w.handleRemove(relPath)
	}
}

func (w *Watcher) handleWrite(fullPath, relPath string) {
	info, err := w.cfg.Stater.Stat(fullPath)
	if err != nil {
		return
	}

	if info.IsDir() {
		w.fsWatcher.Add(fullPath) //nolint:errcheck
		return
	}

	sha, err := w.calculateHash(fullPath)
	if err != nil {
		return
	}

	w.mu.RLock()
	oldHash := w.fileHashes[relPath]
	w.mu.RUnlock()

	if sha == oldHash {
		return
	}

	w.mu.Lock()
	w.fileHashes[relPath] = sha
	w.mu.Unlock()

	select {
	case w.events <- Event{
		RelPath: relPath,
		Op:      OpWrite,
		SHA256:  sha,
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}:
	default:
	}
}

func (w *Watcher) handleRemove(relPath string) {
	w.mu.Lock()
	delete(w.fileHashes, relPath)
	w.mu.Unlock()

	select {
	case w.events <- Event{
		RelPath: relPath,
		Op:      OpRemove,
	}:
	default:
	}
}

func (w *Watcher) calculateHash(path string) (string, error) {
	f, err := w.cfg.Opener.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
