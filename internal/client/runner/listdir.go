package runner

import (
	"context"
	"errors"
	"sort"

	"github.com/HMasataka/axion/internal/clientfs"
	"github.com/HMasataka/axion/internal/proto"
)

// HandleListDir は ListDirRequest を Jail 経由で実行し ListDirResponse を返す。
func (r *Runner) HandleListDir(ctx context.Context, req proto.ListDirRequest) proto.ListDirResponse {
	entries, err := r.cfg.Jail.ReadDir(req.RelPath)
	if err != nil {
		if errors.Is(err, clientfs.ErrPathEscape) {
			return proto.ListDirResponse{Error: "path escape: " + req.RelPath}
		}
		return proto.ListDirResponse{Error: err.Error()}
	}

	out := make([]proto.DirEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, proto.DirEntry{
			Name:    e.Name(),
			IsDir:   e.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().UnixNano(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return proto.ListDirResponse{Entries: out}
}
