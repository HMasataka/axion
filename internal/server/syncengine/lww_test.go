package syncengine

import (
	"testing"
	"time"
)

func strPtr(s string) *string { return &s }

func TestLWW_OtherStateNil_Dispatch(t *testing.T) {
	// Given: otherState=nil
	ev := fileStateView{Op: "write", SHA256: strPtr("abc"), ServerModTime: time.Now().UnixNano()}

	// When
	dec := decideLWW("a", ev, nil, defaultConflictWindowNs)

	// Then: Dispatch
	if dec.Action != LWWActionDispatch {
		t.Errorf("want LWWActionDispatch, got %d", dec.Action)
	}
}

func TestLWW_SameSHA_None(t *testing.T) {
	// Given: 両側 write で sha 一致
	sha := strPtr("samehash")
	now := time.Now().UnixNano()
	ev := fileStateView{Op: "write", SHA256: sha, ServerModTime: now}
	other := &fileStateView{Op: "write", SHA256: strPtr("samehash"), ServerModTime: now - int64(10*time.Second)}

	// When
	dec := decideLWW("a", ev, other, defaultConflictWindowNs)

	// Then: None
	if dec.Action != LWWActionNone {
		t.Errorf("want LWWActionNone, got %d", dec.Action)
	}
}

func TestLWW_DifferentSHA_OutsideWindow_Dispatch(t *testing.T) {
	// Given: sha 不一致 + server_mod_time 差が 10 sec (ConflictWindow=5sec の外)
	now := time.Now().UnixNano()
	ev := fileStateView{Op: "write", SHA256: strPtr("newhash"), ServerModTime: now}
	other := &fileStateView{Op: "write", SHA256: strPtr("oldhash"), ServerModTime: now - int64(10*time.Second)}

	// When
	dec := decideLWW("a", ev, other, defaultConflictWindowNs)

	// Then: Dispatch
	if dec.Action != LWWActionDispatch {
		t.Errorf("want LWWActionDispatch, got %d", dec.Action)
	}
}

func TestLWW_DifferentSHA_InsideWindow_Conflict(t *testing.T) {
	// Given: sha 不一致 + server_mod_time 差が 1 sec (ConflictWindow=5sec の内)
	now := time.Now().UnixNano()
	ev := fileStateView{Op: "write", SHA256: strPtr("newhash"), ServerModTime: now}
	other := &fileStateView{Op: "write", SHA256: strPtr("oldhash"), ServerModTime: now - int64(1*time.Second)}

	// When
	dec := decideLWW("a", ev, other, defaultConflictWindowNs)

	// Then: Conflict
	if dec.Action != LWWActionConflict {
		t.Errorf("want LWWActionConflict, got %d", dec.Action)
	}
}

func TestLWW_DeleteVsWrite_EditWins_OtherIsWrite(t *testing.T) {
	// Given: ev.op=delete + otherState.op=write
	now := time.Now().UnixNano()
	ev := fileStateView{Op: "delete", SHA256: nil, ServerModTime: now}
	other := &fileStateView{Op: "write", SHA256: strPtr("somehash"), ServerModTime: now - int64(20*time.Second)}

	// When
	dec := decideLWW("a", ev, other, defaultConflictWindowNs)

	// Then: EditWins
	if dec.Action != LWWActionEditWins {
		t.Errorf("want LWWActionEditWins, got %d", dec.Action)
	}
}

func TestLWW_DeleteVsWrite_EditWins_OtherIsDelete(t *testing.T) {
	// Given: ev.op=write + otherState.op=delete
	now := time.Now().UnixNano()
	ev := fileStateView{Op: "write", SHA256: strPtr("newhash"), ServerModTime: now}
	other := &fileStateView{Op: "delete", SHA256: nil, ServerModTime: now - int64(20*time.Second)}

	// When
	dec := decideLWW("a", ev, other, defaultConflictWindowNs)

	// Then: EditWins
	if dec.Action != LWWActionEditWins {
		t.Errorf("want LWWActionEditWins, got %d", dec.Action)
	}
}

func TestLWW_BothDelete_None(t *testing.T) {
	// Given: 両側 delete
	now := time.Now().UnixNano()
	ev := fileStateView{Op: "delete", SHA256: nil, ServerModTime: now}
	other := &fileStateView{Op: "delete", SHA256: nil, ServerModTime: now - int64(3*time.Second)}

	// When
	dec := decideLWW("a", ev, other, defaultConflictWindowNs)

	// Then: None
	if dec.Action != LWWActionNone {
		t.Errorf("want LWWActionNone, got %d", dec.Action)
	}
}

func TestLWW_OtherSide_Helper(t *testing.T) {
	// Given/When/Then
	if otherSide("a") != "b" {
		t.Errorf("otherSide(a): want b, got %s", otherSide("a"))
	}
	if otherSide("b") != "a" {
		t.Errorf("otherSide(b): want a, got %s", otherSide("b"))
	}
}
