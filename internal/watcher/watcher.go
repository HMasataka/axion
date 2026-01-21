package watcher

import (
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

type Event struct {
	RelativePath string
	FullPath     string
	Op           Op
	IsDir        bool
	Hash         string
	ModTime      time.Time
	Size         int64
}

type Op int

const (
	OpCreate Op = iota
	OpWrite
	OpRemove
	OpRename
)

type Watcher struct {
	basePath   string
	fsWatcher  *fsnotify.Watcher
	events     chan Event
	errors     chan error
	done       chan struct{}
	ignoreList []string
	mu         sync.RWMutex
	fileHashes map[string]string
}

func New(basePath string, ignoreList []string) (*Watcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		basePath:   basePath,
		fsWatcher:  fsWatcher,
		events:     make(chan Event, 100),
		errors:     make(chan error, 10),
		done:       make(chan struct{}),
		ignoreList: ignoreList,
		fileHashes: make(map[string]string),
	}

	return w, nil
}

func (w *Watcher) Start() error {
	if err := w.addDirRecursive(w.basePath); err != nil {
		return err
	}

	go w.watchLoop()
	return nil
}

func (w *Watcher) Stop() {
	close(w.done)
	w.fsWatcher.Close()
}

func (w *Watcher) Events() <-chan Event {
	return w.events
}

func (w *Watcher) Errors() <-chan error {
	return w.errors
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
	relPath, err := filepath.Rel(w.basePath, path)
	if err != nil {
		return false
	}

	for _, pattern := range w.ignoreList {
		if matched, _ := filepath.Match(pattern, filepath.Base(relPath)); matched {
			return true
		}
		if strings.HasPrefix(relPath, pattern) {
			return true
		}
	}

	return false
}

func (w *Watcher) watchLoop() {
	debouncer := make(map[string]*time.Timer)
	var debounceMu sync.Mutex

	for {
		select {
		case <-w.done:
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

			debounceMu.Lock()
			if timer, exists := debouncer[fsEvent.Name]; exists {
				timer.Stop()
			}

			eventCopy := fsEvent
			debouncer[fsEvent.Name] = time.AfterFunc(100*time.Millisecond, func() {
				w.processEvent(eventCopy)
				debounceMu.Lock()
				delete(debouncer, eventCopy.Name)
				debounceMu.Unlock()
			})
			debounceMu.Unlock()
		}
	}
}

func (w *Watcher) processEvent(fsEvent fsnotify.Event) {
	relPath, err := filepath.Rel(w.basePath, fsEvent.Name)
	if err != nil {
		return
	}

	event := Event{
		RelativePath: filepath.ToSlash(relPath),
		FullPath:     fsEvent.Name,
	}

	info, statErr := os.Stat(fsEvent.Name)

	switch {
	case fsEvent.Op&fsnotify.Create == fsnotify.Create:
		if statErr != nil {
			return
		}
		event.Op = OpCreate
		event.IsDir = info.IsDir()
		event.ModTime = info.ModTime()
		event.Size = info.Size()

		if info.IsDir() {
			w.fsWatcher.Add(fsEvent.Name)
		} else {
			hash, _ := w.calculateHash(fsEvent.Name)
			event.Hash = hash
			w.mu.Lock()
			w.fileHashes[relPath] = hash
			w.mu.Unlock()
		}

	case fsEvent.Op&fsnotify.Write == fsnotify.Write:
		if statErr != nil {
			return
		}
		event.Op = OpWrite
		event.IsDir = info.IsDir()
		event.ModTime = info.ModTime()
		event.Size = info.Size()

		if !info.IsDir() {
			hash, _ := w.calculateHash(fsEvent.Name)
			w.mu.RLock()
			oldHash := w.fileHashes[relPath]
			w.mu.RUnlock()

			if hash == oldHash {
				return
			}

			event.Hash = hash
			w.mu.Lock()
			w.fileHashes[relPath] = hash
			w.mu.Unlock()
		}

	case fsEvent.Op&fsnotify.Remove == fsnotify.Remove:
		event.Op = OpRemove
		w.mu.Lock()
		delete(w.fileHashes, relPath)
		w.mu.Unlock()

	case fsEvent.Op&fsnotify.Rename == fsnotify.Rename:
		event.Op = OpRename
		w.mu.Lock()
		delete(w.fileHashes, relPath)
		w.mu.Unlock()

	default:
		return
	}

	select {
	case w.events <- event:
	default:
	}
}

func (w *Watcher) calculateHash(path string) (string, error) {
	f, err := os.Open(path)
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

func (w *Watcher) GetFileHash(relPath string) string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.fileHashes[relPath]
}

func (w *Watcher) SetFileHash(relPath, hash string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fileHashes[relPath] = hash
}
