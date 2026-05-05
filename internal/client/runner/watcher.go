package runner

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/HMasataka/axion/internal/clientfs"
	"github.com/HMasataka/axion/internal/proto"
	"github.com/HMasataka/axion/internal/watcher"
)

// WatchConfig は WatcherRunner の設定。
type WatchConfig struct {
	Jail       *clientfs.Jail
	Registry   *Registry
	IgnoreList []string
}

// WatcherRunner は jail 経由で watcher を起動し、proto.FileChangedEvent を生成する。
type WatcherRunner struct {
	cfg WatchConfig
	w   *watcher.Watcher
	out chan proto.FileChangedEvent
	err chan error
}

// NewWatcherRunner は新しい WatcherRunner を作る。
func NewWatcherRunner(cfg WatchConfig) (*WatcherRunner, error) {
	w, err := watcher.New(watcher.Config{
		Root:          cfg.Jail.Root(),
		IgnoreList:    cfg.IgnoreList,
		DebounceDelay: 0,
		Opener:        jailOpener{j: cfg.Jail},
		Stater:        jailStater{j: cfg.Jail},
	})
	if err != nil {
		return nil, err
	}
	return &WatcherRunner{
		cfg: cfg,
		w:   w,
		out: make(chan proto.FileChangedEvent, 64),
		err: make(chan error, 8),
	}, nil
}

// Start は内部 watcher を起動し、変換 goroutine を開始する。
func (wr *WatcherRunner) Start(ctx context.Context) error {
	if err := wr.w.Start(ctx); err != nil {
		return err
	}
	go wr.translateLoop(ctx)
	go wr.errorPump(ctx)
	return nil
}

// Events は変換済み FileChangedEvent チャネル。
func (wr *WatcherRunner) Events() <-chan proto.FileChangedEvent {
	return wr.out
}

// Errors は内部エラーチャネル。
func (wr *WatcherRunner) Errors() <-chan error {
	return wr.err
}

// Stop は監視停止。
func (wr *WatcherRunner) Stop() error {
	return wr.w.Stop()
}

func (wr *WatcherRunner) translateLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-wr.w.Events():
			if !ok {
				return
			}
			matches := wr.cfg.Registry.Resolve(ev.RelPath)
			if len(matches) == 0 {
				slog.DebugContext(ctx, "watch event has no matching subscription", "rel", ev.RelPath)
				continue
			}
			for _, m := range matches {
				op := "write"
				if ev.Op == watcher.OpRemove {
					op = "delete"
				}
				out := proto.FileChangedEvent{
					PairID:  m.PairID,
					Side:    m.Side,
					RelPath: m.SubRel,
					SHA256:  ev.SHA256,
					Size:    ev.Size,
					ModTime: ev.ModTime.UnixNano(),
					Op:      op,
				}
				select {
				case wr.out <- out:
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

func (wr *WatcherRunner) errorPump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-wr.w.Errors():
			if !ok {
				return
			}
			select {
			case wr.err <- e:
			case <-ctx.Done():
				return
			}
		}
	}
}

// jailOpener は clientfs.Jail を watcher.Opener に適合させる。
// watcher 内部は absolute path を渡してくるため、jail root を trim してから j.Open を呼ぶ。
type jailOpener struct{ j *clientfs.Jail }

func (o jailOpener) Open(abs string) (io.ReadCloser, error) {
	rel := strings.TrimPrefix(abs, o.j.Root()+"/")
	return o.j.Open(rel)
}

// jailStater は clientfs.Jail を watcher.Stater に適合させる。
type jailStater struct{ j *clientfs.Jail }

func (s jailStater) Stat(abs string) (os.FileInfo, error) {
	rel := strings.TrimPrefix(abs, s.j.Root()+"/")
	return s.j.Stat(rel)
}
