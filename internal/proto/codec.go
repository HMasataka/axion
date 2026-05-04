package proto

import "encoding/json"

// Envelope は全 WS メッセージの外枠。Type で payload 型を識別、CorrelationID で
// request/response を相関させる（CorrelationID は呼び出し側で UUIDv4 生成）。
type Envelope struct {
	Type          string          `json:"type"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

// Marshal は Envelope を JSON バイト列にする。
func Marshal(e Envelope) ([]byte, error) {
	return json.Marshal(e)
}

// Unmarshal は JSON バイト列から Envelope を復元する。
func Unmarshal(data []byte) (Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return Envelope{}, err
	}
	return e, nil
}

// MarshalPayload は型付き payload を json.RawMessage に変換する。
func MarshalPayload(v any) (json.RawMessage, error) {
	return json.Marshal(v)
}

// UnmarshalPayload は json.RawMessage を型付き payload に復元する。
func UnmarshalPayload(raw json.RawMessage, v any) error {
	return json.Unmarshal(raw, v)
}
