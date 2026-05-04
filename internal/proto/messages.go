package proto

const (
	TypeRegisterRequest   = "register_request"
	TypeRegisterResponse  = "register_response"
	TypeSubscribePair     = "subscribe_pair"
	TypeSubscribePairAck  = "subscribe_pair_ack"
	TypeFileChangedEvent  = "file_changed_event"
	TypeFileSyncCommand   = "file_sync_command"
	TypeFileSyncAck       = "file_sync_ack"
	TypeListDirRequest    = "list_dir_request"
	TypeListDirResponse   = "list_dir_response"
	TypeListFilesRequest  = "list_files_request"
	TypeListFilesResponse = "list_files_response"
	TypePing              = "ping"
	TypePong              = "pong"
)

type RegisterRequest struct {
	ClientID     string `json:"client_id"`
	Hostname     string `json:"hostname"`
	RootPath     string `json:"root_path"`
	Version      string `json:"version"`
	ProtoVersion string `json:"proto_version"`
}

type RegisterResponse struct {
	OK         bool              `json:"ok"`
	ServerTime int64             `json:"server_time"`        // unix nano
	Settings   map[string]string `json:"settings,omitempty"` // ignore_list 等のキー/値
	Reason     string            `json:"reason,omitempty"`   // OK=false 時の理由
}

type SubscribePair struct {
	PairID      string `json:"pair_id"`
	Side        string `json:"side"`         // "a" | "b"
	RootSubpath string `json:"root_subpath"` // クライアント root からの相対
	Direction   string `json:"direction"`    // "bidirectional" | "a_to_b" | "b_to_a"
}

type SubscribePairAck struct {
	PairID string `json:"pair_id"`
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

type FileChangedEvent struct {
	PairID  string `json:"pair_id"`
	Side    string `json:"side"`
	RelPath string `json:"rel_path"`
	SHA256  string `json:"sha256,omitempty"` // 削除時は空
	Size    int64  `json:"size,omitempty"`
	ModTime int64  `json:"mod_time"`
	Op      string `json:"op"` // "write" | "delete"
	IsDir   bool   `json:"is_dir,omitempty"`
}

type FileSyncCommand struct {
	PairID             string `json:"pair_id"`
	Side               string `json:"side"`
	RelPath            string `json:"rel_path"`
	SHA256             string `json:"sha256,omitempty"`
	Op                 string `json:"op"`                       // "fetch" | "rename" | "delete"
	NewRelPath         string `json:"new_rel_path,omitempty"`   // op=rename 時
	OriginatorClientID string `json:"originator_client_id"`
}

type FileSyncAck struct {
	PairID  string `json:"pair_id"`
	Side    string `json:"side"`
	RelPath string `json:"rel_path"`
	SHA256  string `json:"sha256,omitempty"`
	Status  string `json:"status"` // "ok" | "failed"
	Error   string `json:"error,omitempty"`
}

type ListDirRequest struct {
	RelPath string `json:"rel_path"`
}

type DirEntry struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size,omitempty"`
	ModTime int64  `json:"mod_time,omitempty"`
}

type ListDirResponse struct {
	Entries []DirEntry `json:"entries"`
	Error   string     `json:"error,omitempty"`
}

type ListFilesRequest struct {
	PairID string `json:"pair_id"`
	Side   string `json:"side"`
}

type FileSnapshot struct {
	RelPath string `json:"rel_path"`
	SHA256  string `json:"sha256,omitempty"`
	Size    int64  `json:"size,omitempty"`
	ModTime int64  `json:"mod_time"`
	IsDir   bool   `json:"is_dir,omitempty"`
}

type ListFilesResponse struct {
	Entries []FileSnapshot `json:"entries"`
}

type Ping struct{}
type Pong struct{}
