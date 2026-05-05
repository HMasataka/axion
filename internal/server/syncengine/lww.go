package syncengine

import "time"

const defaultConflictWindowNs int64 = int64(5 * time.Second)

// LWWAction は双方向 LWW の判定結果の種別。
type LWWAction int

const (
	LWWActionNone     LWWAction = iota // dispatch しない
	LWWActionDispatch                  // 通常の片方向 dispatch (相手は古い)
	LWWActionConflict                  // 同時変更検出 → conflict 退避
	// LWWActionDeleteWins は削除側が勝つケース (現仕様では EditWins を使用)
	LWWActionDeleteWins LWWAction = iota + 1
	LWWActionEditWins               // 編集側を勝者として削除側に再配布
)

// LWWDecision は双方向 LWW の判定結果。
type LWWDecision struct {
	Action      LWWAction
	OtherSide   string // "a" or "b" — 相手サイド
	OtherClient string // 相手 client_id
	OtherPath   string // 相手 root_subpath (path_a or path_b)
	LosingSHA   string // ConflictRename の場合の負け側 sha
	LosingMTime int64  // 負け側 server_mod_time
}

// otherSide は "a" → "b", "b" → "a" を返す。
func otherSide(s string) string {
	if s == "a" {
		return "b"
	}
	return "a"
}

// decideLWW は src 側の最新変更 ev と otherState を比較して何をすべきかを返す。
//
// 入力前提: ev は既に store.UpsertFileState で永続化済み (server_mod_time 打刻済み)。
//
// ロジック:
//   - otherState が nil (相手側未登録) → Dispatch
//   - 両側 delete → None
//   - ev=delete + other=write → EditWins (write 側が勝ち)
//   - ev=write + other=delete → EditWins (write 側が勝ち)
//   - 両側 write で sha 一致 → None
//   - 両側 write で sha 不一致 + ConflictWindow 内 → Conflict
//   - 両側 write で sha 不一致 + ConflictWindow 外 → Dispatch (新しい方が勝ち)
func decideLWW(srcSide string, ev fileStateView, otherState *fileStateView, conflictWindowNs int64) LWWDecision {
	if otherState == nil {
		return LWWDecision{Action: LWWActionDispatch}
	}

	evOp := ev.Op
	otherOp := otherState.Op

	if evOp == "delete" && otherOp == "delete" {
		return LWWDecision{Action: LWWActionNone}
	}

	if evOp == "delete" && otherOp == "write" {
		return LWWDecision{Action: LWWActionEditWins}
	}

	if evOp == "write" && otherOp == "delete" {
		return LWWDecision{Action: LWWActionEditWins}
	}

	// 両側 write
	if ev.SHA256 != nil && otherState.SHA256 != nil && *ev.SHA256 == *otherState.SHA256 {
		return LWWDecision{Action: LWWActionNone}
	}

	diff := ev.ServerModTime - otherState.ServerModTime
	if diff < 0 {
		diff = -diff
	}
	if diff < conflictWindowNs {
		return LWWDecision{Action: LWWActionConflict}
	}

	return LWWDecision{Action: LWWActionDispatch}
}

// fileStateView は decideLWW が必要とする FileState の最小ビュー。
// store.FileState を直接使わず変換することでテストが容易になる。
type fileStateView struct {
	Op            string
	SHA256        *string
	ServerModTime int64
}
