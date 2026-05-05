package syncengine

import (
	"github.com/HMasataka/axion/internal/server/store"
)

// Target は同期配布先。
type Target struct {
	ClientID string
	Side     string // "a" | "b"
	Path     string // クライアント root からの相対 (path_a or path_b)
}

// resolveMirrorTargets は片方向 mirror で配布先を決定する。
// direction="a_to_b" + srcSide="a" → [{ClientB, "b", PathB}]
// direction="a_to_b" + srcSide="b" → [] (B 側変更は無視)
// direction="b_to_a" + srcSide="b" → [{ClientA, "a", PathA}]
// direction="b_to_a" + srcSide="a" → []
// direction="bidirectional" → 呼び出し元で判定（本関数では扱わない）
func resolveMirrorTargets(p store.SyncPair, srcSide string) []Target {
	switch p.Direction {
	case "a_to_b":
		if srcSide == "a" {
			return []Target{{ClientID: p.ClientBID, Side: "b", Path: p.PathB}}
		}
		return nil
	case "b_to_a":
		if srcSide == "b" {
			return []Target{{ClientID: p.ClientAID, Side: "a", Path: p.PathA}}
		}
		return nil
	default:
		return nil
	}
}
