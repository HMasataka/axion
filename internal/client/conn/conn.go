package conn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/HMasataka/axion/internal/proto"
)

// ErrNotConnected は Send が未接続状態で呼ばれたときに返す。
var ErrNotConnected = errors.New("conn: not connected")

// Config は WS クライアント設定。
type Config struct {
	ServerURL string
	PSK       string
	ClientID  string
	Hostname  string
	RootPath  string
	Version   string
}

// Conn は再接続を含むクライアント側の WS 接続管理。
type Conn struct {
	cfg    Config
	bo     *ExponentialBackoff
	mu     sync.Mutex
	ws     *websocket.Conn
	closed chan struct{}
}

func New(cfg Config) *Conn {
	return &Conn{
		cfg:    cfg,
		bo:     NewExponentialBackoff(),
		closed: make(chan struct{}),
	}
}

// Run は再接続ループを実行する。ctx canceled で終了。
// onConnected は新規接続成功＆RegisterResponse 受信完了時に呼ばれる。
// onMessage はサーバーからのメッセージを受信したときに呼ばれる（Pong は内部処理）。
func (c *Conn) Run(ctx context.Context, onConnected func(settings map[string]string), onMessage func(env proto.Envelope) error) error {
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		ws, err := c.dial(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			wait := c.bo.Next()
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(wait):
			}
			continue
		}

		c.mu.Lock()
		c.ws = ws
		c.mu.Unlock()

		c.bo.Reset()

		settings, err := c.register(ctx, ws)
		if err != nil {
			ws.Close(websocket.StatusInternalError, "register failed")
			c.mu.Lock()
			c.ws = nil
			c.mu.Unlock()
			if ctx.Err() != nil {
				return nil
			}
			wait := c.bo.Next()
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(wait):
			}
			continue
		}

		onConnected(settings)

		readErr := c.readLoop(ctx, ws, onMessage)

		ws.Close(websocket.StatusNormalClosure, "closing")
		c.mu.Lock()
		c.ws = nil
		c.mu.Unlock()

		if ctx.Err() != nil || errors.Is(readErr, context.Canceled) {
			return nil
		}

		wait := c.bo.Next()
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

// Send はサーバーへ Envelope を送信する。未接続なら ErrNotConnected。
func (c *Conn) Send(ctx context.Context, env proto.Envelope) error {
	c.mu.Lock()
	ws := c.ws
	c.mu.Unlock()

	if ws == nil {
		return ErrNotConnected
	}

	data, err := proto.Marshal(env)
	if err != nil {
		return fmt.Errorf("conn: marshal: %w", err)
	}

	return ws.Write(ctx, websocket.MessageText, data)
}

// Close は接続を閉じる。
func (c *Conn) Close() {
	close(c.closed)

	c.mu.Lock()
	ws := c.ws
	c.mu.Unlock()

	if ws != nil {
		ws.Close(websocket.StatusNormalClosure, "client closed")
	}
}

func (c *Conn) dial(ctx context.Context) (*websocket.Conn, error) {
	ws, _, err := websocket.Dial(ctx, c.cfg.ServerURL+"/v1/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": []string{"Bearer " + c.cfg.PSK},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("conn: dial: %w", err)
	}
	ws.SetReadLimit(64 * 1024)
	return ws, nil
}

func (c *Conn) register(ctx context.Context, ws *websocket.Conn) (map[string]string, error) {
	payload, err := proto.MarshalPayload(proto.RegisterRequest{
		ClientID:     c.cfg.ClientID,
		Hostname:     c.cfg.Hostname,
		RootPath:     c.cfg.RootPath,
		Version:      c.cfg.Version,
		ProtoVersion: proto.ProtoVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("conn: marshal register request: %w", err)
	}

	env := proto.Envelope{
		Type:    proto.TypeRegisterRequest,
		Payload: payload,
	}

	data, err := proto.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("conn: marshal envelope: %w", err)
	}

	if err := ws.Write(ctx, websocket.MessageText, data); err != nil {
		return nil, fmt.Errorf("conn: send register request: %w", err)
	}

	regCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, raw, err := ws.Read(regCtx)
	if err != nil {
		return nil, fmt.Errorf("conn: read register response: %w", err)
	}

	resp, err := proto.Unmarshal(raw)
	if err != nil {
		return nil, fmt.Errorf("conn: unmarshal register response: %w", err)
	}

	if resp.Type != proto.TypeRegisterResponse {
		return nil, fmt.Errorf("conn: expected register_response, got %q", resp.Type)
	}

	var regResp proto.RegisterResponse
	if err := json.Unmarshal(resp.Payload, &regResp); err != nil {
		return nil, fmt.Errorf("conn: decode register response payload: %w", err)
	}

	if !regResp.OK {
		return nil, fmt.Errorf("conn: register rejected: %s", regResp.Reason)
	}

	return regResp.Settings, nil
}

func (c *Conn) readLoop(ctx context.Context, ws *websocket.Conn, onMessage func(env proto.Envelope) error) error {
	for {
		_, raw, err := ws.Read(ctx)
		if err != nil {
			return err
		}

		env, err := proto.Unmarshal(raw)
		if err != nil {
			continue
		}

		if env.Type == proto.TypePong {
			continue
		}

		if err := onMessage(env); err != nil {
			return err
		}
	}
}
