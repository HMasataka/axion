package protocol

import (
	"encoding/binary"
	"encoding/json"
	"io"
)

type MessageType uint8

const (
	TypeFileChange MessageType = iota + 1
	TypeFileRequest
	TypeFileData
	TypeFileDelete
	TypeSyncRequest
	TypeSyncResponse
	TypeAck
)

type Message struct {
	Type    MessageType `json:"type"`
	Payload []byte      `json:"payload"`
}

type FileChangePayload struct {
	RelativePath string `json:"relative_path"`
	Hash         string `json:"hash"`
	ModTime      int64  `json:"mod_time"`
	Size         int64  `json:"size"`
	IsDir        bool   `json:"is_dir"`
}

type FileRequestPayload struct {
	RelativePath string `json:"relative_path"`
}

type FileDataPayload struct {
	RelativePath string `json:"relative_path"`
	Data         []byte `json:"data"`
	ModTime      int64  `json:"mod_time"`
}

type FileDeletePayload struct {
	RelativePath string `json:"relative_path"`
	IsDir        bool   `json:"is_dir"`
}

type SyncRequestPayload struct {
	Files []FileInfo `json:"files"`
}

type SyncResponsePayload struct {
	NeedFiles   []string `json:"need_files"`
	DeleteFiles []string `json:"delete_files"`
}

type FileInfo struct {
	RelativePath string `json:"relative_path"`
	Hash         string `json:"hash"`
	ModTime      int64  `json:"mod_time"`
	Size         int64  `json:"size"`
	IsDir        bool   `json:"is_dir"`
}

func Encode(msg *Message) ([]byte, error) {
	payload, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}

	length := uint32(len(payload))
	buf := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(buf[:4], length)
	copy(buf[4:], payload)

	return buf, nil
}

func Decode(r io.Reader) (*Message, error) {
	lengthBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lengthBuf); err != nil {
		return nil, err
	}

	length := binary.BigEndian.Uint32(lengthBuf)
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}

	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		return nil, err
	}

	return &msg, nil
}

func NewFileChangeMessage(payload *FileChangePayload) (*Message, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &Message{Type: TypeFileChange, Payload: data}, nil
}

func NewFileRequestMessage(payload *FileRequestPayload) (*Message, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &Message{Type: TypeFileRequest, Payload: data}, nil
}

func NewFileDataMessage(payload *FileDataPayload) (*Message, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &Message{Type: TypeFileData, Payload: data}, nil
}

func NewFileDeleteMessage(payload *FileDeletePayload) (*Message, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &Message{Type: TypeFileDelete, Payload: data}, nil
}

func NewSyncRequestMessage(payload *SyncRequestPayload) (*Message, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &Message{Type: TypeSyncRequest, Payload: data}, nil
}

func NewSyncResponseMessage(payload *SyncResponsePayload) (*Message, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &Message{Type: TypeSyncResponse, Payload: data}, nil
}

func ParseFileChangePayload(data []byte) (*FileChangePayload, error) {
	var payload FileChangePayload
	err := json.Unmarshal(data, &payload)
	return &payload, err
}

func ParseFileRequestPayload(data []byte) (*FileRequestPayload, error) {
	var payload FileRequestPayload
	err := json.Unmarshal(data, &payload)
	return &payload, err
}

func ParseFileDataPayload(data []byte) (*FileDataPayload, error) {
	var payload FileDataPayload
	err := json.Unmarshal(data, &payload)
	return &payload, err
}

func ParseFileDeletePayload(data []byte) (*FileDeletePayload, error) {
	var payload FileDeletePayload
	err := json.Unmarshal(data, &payload)
	return &payload, err
}

func ParseSyncRequestPayload(data []byte) (*SyncRequestPayload, error) {
	var payload SyncRequestPayload
	err := json.Unmarshal(data, &payload)
	return &payload, err
}

func ParseSyncResponsePayload(data []byte) (*SyncResponsePayload, error) {
	var payload SyncResponsePayload
	err := json.Unmarshal(data, &payload)
	return &payload, err
}
