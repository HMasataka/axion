package web

import (
	"bytes"
	"context"
	"html/template"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Broadcaster は接続中の Web UI クライアントへ HTML フラグメントを push する。
type Broadcaster struct {
	mu   sync.RWMutex
	subs map[chan []byte]struct{}
}

// NewBroadcaster は Broadcaster を初期化して返す。
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{subs: make(map[chan []byte]struct{})}
}

// Subscribe は新しい subscriber チャネルを追加する。返り値の cancel で解除。
func (b *Broadcaster) Subscribe() (chan []byte, func()) {
	ch := make(chan []byte, 16)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
		close(ch)
	}
}

// Publish は HTML フラグメントを全 subscriber に送信する。
// 受信側のチャネルが満杯なら drop する (slow consumer は無視)。
func (b *Broadcaster) Publish(html []byte) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- html:
		default:
			// slow consumer: drop
		}
	}
}

// PublishClientStatus は client の online/offline 変化を全 subscriber に push する。
func (b *Broadcaster) PublishClientStatus(tmpl *template.Template, view ClientView) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "client_row_with_tr", view); err != nil {
		return
	}
	b.Publish(buf.Bytes())
}

// adminWSHandler は /admin/ws の HTMX subscriber 接続を受け付ける。
func adminWSHandler(b *Broadcaster) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		defer c.CloseNow()

		ch, cancel := b.Subscribe()
		defer cancel()

		ctx, cancelCtx := context.WithCancel(r.Context())
		defer cancelCtx()

		go func() {
			for {
				_, _, err := c.Read(ctx)
				if err != nil {
					cancelCtx()
					return
				}
			}
		}()

		t := time.NewTicker(20 * time.Second)
		defer t.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				if err := c.Write(ctx, websocket.MessageText, msg); err != nil {
					return
				}
			case <-t.C:
				if err := c.Ping(ctx); err != nil {
					return
				}
			}
		}
	})
}
