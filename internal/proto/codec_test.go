package proto

import (
	"reflect"
	"testing"
)

func roundTrip[T any](t *testing.T, msgType string, given T) {
	t.Helper()

	// When: MarshalPayload → Envelope → Marshal → Unmarshal → UnmarshalPayload
	raw, err := MarshalPayload(given)
	if err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}

	env := Envelope{
		Type:          msgType,
		CorrelationID: "test-corr-id",
		Payload:       raw,
	}

	data, err := Marshal(env)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	var result T
	if err := UnmarshalPayload(got.Payload, &result); err != nil {
		t.Fatalf("UnmarshalPayload: %v", err)
	}

	// Then: reflect.DeepEqual で一致
	if !reflect.DeepEqual(given, result) {
		t.Errorf("round-trip mismatch:\ngiven:  %+v\nresult: %+v", given, result)
	}

	if got.Type != msgType {
		t.Errorf("type mismatch: want %q, got %q", msgType, got.Type)
	}
}

func TestRoundTrip_RegisterRequest(t *testing.T) {
	given := RegisterRequest{
		ClientID:     "client-1",
		Hostname:     "host.example.com",
		RootPath:     "/home/user/sync",
		Version:      "0.1.0",
		ProtoVersion: ProtoVersion,
	}
	roundTrip(t, TypeRegisterRequest, given)
}

func TestRoundTrip_RegisterResponse(t *testing.T) {
	given := RegisterResponse{
		OK:         true,
		ServerTime: 1700000000000000000,
		Settings:   map[string]string{"ignore_list": ".git,.DS_Store"},
	}
	roundTrip(t, TypeRegisterResponse, given)
}

func TestRoundTrip_SubscribePair(t *testing.T) {
	given := SubscribePair{
		PairID:      "pair-abc",
		Side:        "a",
		RootSubpath: "docs/",
		Direction:   "bidirectional",
	}
	roundTrip(t, TypeSubscribePair, given)
}

func TestRoundTrip_SubscribePairAck(t *testing.T) {
	given := SubscribePairAck{
		PairID: "pair-abc",
		OK:     true,
	}
	roundTrip(t, TypeSubscribePairAck, given)
}

func TestRoundTrip_FileChangedEvent(t *testing.T) {
	given := FileChangedEvent{
		PairID:  "pair-abc",
		Side:    "a",
		RelPath: "docs/notes.txt",
		SHA256:  "abc123",
		Size:    1024,
		ModTime: 1700000000,
		Op:      "write",
		IsDir:   false,
	}
	roundTrip(t, TypeFileChangedEvent, given)
}

func TestRoundTrip_FileSyncCommand(t *testing.T) {
	given := FileSyncCommand{
		PairID:             "pair-abc",
		Side:               "b",
		RelPath:            "docs/notes.txt",
		SHA256:             "abc123",
		Op:                 "fetch",
		OriginatorClientID: "client-1",
	}
	roundTrip(t, TypeFileSyncCommand, given)
}

func TestRoundTrip_FileSyncAck(t *testing.T) {
	given := FileSyncAck{
		PairID:  "pair-abc",
		Side:    "b",
		RelPath: "docs/notes.txt",
		SHA256:  "abc123",
		Status:  "ok",
	}
	roundTrip(t, TypeFileSyncAck, given)
}

func TestRoundTrip_ListDirRequest(t *testing.T) {
	given := ListDirRequest{
		RelPath: "docs/",
	}
	roundTrip(t, TypeListDirRequest, given)
}

func TestRoundTrip_ListDirResponse(t *testing.T) {
	given := ListDirResponse{
		Entries: []DirEntry{
			{Name: "notes.txt", IsDir: false, Size: 1024, ModTime: 1700000000},
			{Name: "images", IsDir: true},
		},
	}
	roundTrip(t, TypeListDirResponse, given)
}

func TestRoundTrip_ListFilesRequest(t *testing.T) {
	given := ListFilesRequest{
		PairID: "pair-abc",
		Side:   "a",
	}
	roundTrip(t, TypeListFilesRequest, given)
}

func TestRoundTrip_ListFilesResponse(t *testing.T) {
	given := ListFilesResponse{
		Entries: []FileSnapshot{
			{RelPath: "docs/notes.txt", SHA256: "abc123", Size: 1024, ModTime: 1700000000},
			{RelPath: "docs/images", IsDir: true, ModTime: 1700000001},
		},
	}
	roundTrip(t, TypeListFilesResponse, given)
}

func TestRoundTrip_Ping(t *testing.T) {
	roundTrip(t, TypePing, Ping{})
}

func TestRoundTrip_Pong(t *testing.T) {
	roundTrip(t, TypePong, Pong{})
}

func TestUnknownField_Tolerated(t *testing.T) {
	// Given: 既知型に未知 JSON キーを含むバイト列
	jsonWithUnknown := []byte(`{"client_id":"c1","hostname":"h1","root_path":"/r","version":"v1","proto_version":"1","unknown_key":"ignored"}`)

	// When: UnmarshalPayload
	var result RegisterRequest
	err := UnmarshalPayload(jsonWithUnknown, &result)

	// Then: エラーにならない
	if err != nil {
		t.Fatalf("expected no error for unknown field, got: %v", err)
	}
	if result.ClientID != "c1" {
		t.Errorf("ClientID: want %q, got %q", "c1", result.ClientID)
	}
}

func TestEnvelope_NilPayload(t *testing.T) {
	// Given: Payload が nil の Envelope
	given := Envelope{
		Type:          TypePing,
		CorrelationID: "corr-nil",
	}

	// When: Marshal → Unmarshal
	data, err := Marshal(given)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Then: Type と CorrelationID が一致し、Payload は nil または空
	if got.Type != given.Type {
		t.Errorf("type mismatch: want %q, got %q", given.Type, got.Type)
	}
	if got.CorrelationID != given.CorrelationID {
		t.Errorf("correlation_id mismatch: want %q, got %q", given.CorrelationID, got.CorrelationID)
	}
	if len(got.Payload) != 0 {
		t.Errorf("expected empty payload, got: %s", got.Payload)
	}
}
