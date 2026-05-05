package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/HMasataka/axion/internal/server/web"
	"github.com/coder/websocket"
)

func TestBroadcaster_PublishToSubscriber(t *testing.T) {
	// Given: 1 subscriber
	b := web.NewBroadcaster()
	ch, cancel := b.Subscribe()
	defer cancel()

	// When: publish a message
	msg := []byte("<tr>test</tr>")
	b.Publish(msg)

	// Then: subscriber receives the message
	select {
	case got := <-ch:
		if string(got) != string(msg) {
			t.Fatalf("got %q, want %q", got, msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestBroadcaster_PublishToMultipleSubscribers(t *testing.T) {
	// Given: 2 subscribers
	b := web.NewBroadcaster()
	ch1, cancel1 := b.Subscribe()
	defer cancel1()
	ch2, cancel2 := b.Subscribe()
	defer cancel2()

	// When: publish a message
	msg := []byte("<tr>multi</tr>")
	b.Publish(msg)

	// Then: both subscribers receive the message
	for _, ch := range []chan []byte{ch1, ch2} {
		select {
		case got := <-ch:
			if string(got) != string(msg) {
				t.Fatalf("got %q, want %q", got, msg)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for message")
		}
	}
}

func TestBroadcaster_DropOnSlowConsumer(t *testing.T) {
	// Given: a broadcaster with one full subscriber and one normal subscriber
	b := web.NewBroadcaster()

	// fill the slow consumer's buffer (capacity 16)
	slowCh, cancelSlow := b.Subscribe()
	defer cancelSlow()
	fastCh, cancelFast := b.Subscribe()
	defer cancelFast()

	// fill slow subscriber buffer to capacity
	for range 16 {
		slowCh <- []byte("fill")
	}

	// When: publish while slow consumer is full
	msg := []byte("<tr>drop</tr>")
	b.Publish(msg)

	// Then: fast subscriber receives the message (slow consumer drop does not affect others)
	select {
	case got := <-fastCh:
		if string(got) != string(msg) {
			t.Fatalf("got %q, want %q", got, msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message on fast subscriber")
	}

	// slow subscriber does NOT receive the dropped message (buffer was full)
	select {
	case got := <-slowCh:
		// drain the fill messages
		if string(got) != "fill" {
			t.Fatalf("unexpected message on slow subscriber: %q", got)
		}
	default:
	}
}

func TestBroadcaster_UnsubscribeStops(t *testing.T) {
	// Given: a subscriber that unsubscribes
	b := web.NewBroadcaster()
	ch, cancel := b.Subscribe()
	cancel() // unsubscribe immediately

	// When: publish after unsubscribe
	b.Publish([]byte("<tr>after</tr>"))

	// Then: the closed channel should not receive a new message
	// (channel is closed by cancel, so reading returns zero value immediately)
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel, got open channel with data")
		}
		// closed channel: correct behavior
	default:
		// no message: also acceptable (message was dropped because subscriber removed)
	}
}

func TestAdminWS_AcceptsConnection(t *testing.T) {
	// Given: an HTTP test server with adminWSHandler
	b := web.NewBroadcaster()
	h := web.Handler(web.Config{Broadcaster: b})
	srv := httptest.NewServer(h)
	defer srv.Close()

	// When: dial /admin/ws
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + srv.URL[len("http"):] + "/admin/ws"
	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial: %v (status: %v)", err, resp)
	}
	defer conn.CloseNow()

	// Then: connection is established (no error)
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101, got %d", resp.StatusCode)
	}
}

func TestAdminWS_PublishedMessageReceived(t *testing.T) {
	// Given: a connected WebSocket client
	b := web.NewBroadcaster()
	h := web.Handler(web.Config{Broadcaster: b})
	srv := httptest.NewServer(h)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + srv.URL[len("http"):] + "/admin/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	defer conn.CloseNow()

	// When: publish a message via broadcaster
	msg := []byte("<tr id='client-x' hx-swap-oob='outerHTML'><td>test</td></tr>")
	b.Publish(msg)

	// Then: WebSocket client receives the message
	_, got, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("conn.Read: %v", err)
	}
	if string(got) != string(msg) {
		t.Fatalf("got %q, want %q", got, msg)
	}
}
