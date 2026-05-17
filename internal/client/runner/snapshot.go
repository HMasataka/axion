package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/HMasataka/axion/internal/clientfs"
	"github.com/HMasataka/axion/internal/proto"
	"github.com/HMasataka/axion/internal/watcher"
)

const (
	maxScanFiles = 100000
	maxScanDepth = 32
)

// HandleListFiles は ListFilesRequest を受け取り、pair の root_subpath を walk して
// ファイルスナップショットの一覧を返す。
func (r *Runner) HandleListFiles(ctx context.Context, req proto.ListFilesRequest) proto.ListFilesResponse {
	sub, ok := r.lookupSubscription(req.PairID)
	if !ok {
		return proto.ListFilesResponse{}
	}

	root := sub.RootSubpath
	if root == "" {
		root = "."
	}

	var entries []proto.FileSnapshot
	count := 0

	if err := r.walkScan(ctx, root, ".", 0, &entries, &count); err != nil {
		slog.WarnContext(ctx, "snapshot scan", "pair_id", req.PairID, "error", err)
	}

	return proto.ListFilesResponse{Entries: entries}
}

// walkScan は rootSubpath 配下を再帰的に walk して entries に追記する。
// subRel は rootSubpath からの相対パスで FileSnapshot.RelPath に使用する。
func (r *Runner) walkScan(ctx context.Context, rootSubpath, subRel string, depth int, entries *[]proto.FileSnapshot, count *int) error {
	if depth > maxScanDepth {
		slog.WarnContext(ctx, "snapshot scan depth exceeded", "depth", depth)
		return nil
	}
	if *count >= maxScanFiles {
		slog.WarnContext(ctx, "snapshot scan file count exceeded", "count", *count)
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	jailRel := filepath.Join(rootSubpath, subRel)
	if subRel == "." {
		jailRel = rootSubpath
	}

	dirEntries, err := r.cfg.Jail.ReadDir(jailRel)
	if err != nil {
		return err
	}

	for _, e := range dirEntries {
		entrySubRel := filepath.ToSlash(filepath.Join(subRel, e.Name()))
		if subRel == "." {
			entrySubRel = e.Name()
		}
		entryJailRel := filepath.Join(rootSubpath, entrySubRel)

		if watcher.MatchIgnore(entryJailRel, r.cfg.IgnoreList) {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		if e.IsDir() {
			*entries = append(*entries, proto.FileSnapshot{
				RelPath: entrySubRel,
				IsDir:   true,
				ModTime: info.ModTime().UnixNano(),
			})
			*count++
			if err := r.walkScan(ctx, rootSubpath, entrySubRel, depth+1, entries, count); err != nil {
				return err
			}
			continue
		}

		mode := info.Mode()
		if mode&(os.ModeSymlink|os.ModeDevice|os.ModeNamedPipe|os.ModeSocket|os.ModeCharDevice|os.ModeIrregular) != 0 {
			slog.InfoContext(ctx, "skip non-regular file", "rel", entrySubRel, "mode", mode.String())
			continue
		}

		sha, err := r.computeSHA(entryJailRel)
		if err != nil {
			slog.WarnContext(ctx, "sha compute", "rel", entrySubRel, "error", err)
			continue
		}

		*entries = append(*entries, proto.FileSnapshot{
			RelPath: entrySubRel,
			SHA256:  sha,
			Size:    info.Size(),
			ModTime: info.ModTime().UnixNano(),
		})
		*count++
	}
	return nil
}

func (r *Runner) computeSHA(jailRel string) (string, error) {
	f, err := r.cfg.Jail.Open(jailRel)
	if err != nil {
		if errors.Is(err, clientfs.ErrUnsupportedFileType) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
