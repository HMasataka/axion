package peer

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/HMasataka/axion/internal/protocol"
)

type Peer struct {
	addr       string
	conn       net.Conn
	mu         sync.RWMutex
	connected  bool
	incoming   chan *protocol.Message
	outgoing   chan *protocol.Message
	done       chan struct{}
	onMessage  func(*protocol.Message)
	reconnect  bool
	isServer   bool
}

func NewClient(addr string) *Peer {
	return &Peer{
		addr:      addr,
		incoming:  make(chan *protocol.Message, 100),
		outgoing:  make(chan *protocol.Message, 100),
		done:      make(chan struct{}),
		reconnect: true,
		isServer:  false,
	}
}

func NewFromConn(conn net.Conn) *Peer {
	return &Peer{
		addr:      conn.RemoteAddr().String(),
		conn:      conn,
		connected: true,
		incoming:  make(chan *protocol.Message, 100),
		outgoing:  make(chan *protocol.Message, 100),
		done:      make(chan struct{}),
		reconnect: false,
		isServer:  true,
	}
}

func (p *Peer) SetMessageHandler(handler func(*protocol.Message)) {
	p.onMessage = handler
}

func (p *Peer) Connect() error {
	p.mu.Lock()
	if p.connected {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()

	conn, err := net.DialTimeout("tcp", p.addr, 10*time.Second)
	if err != nil {
		return err
	}

	p.mu.Lock()
	p.conn = conn
	p.connected = true
	p.mu.Unlock()

	go p.readLoop()
	go p.writeLoop()

	return nil
}

func (p *Peer) Start() {
	go p.readLoop()
	go p.writeLoop()
}

func (p *Peer) StartWithReconnect() {
	go func() {
		for {
			select {
			case <-p.done:
				return
			default:
				if err := p.Connect(); err != nil {
					slog.Warn("Failed to connect", "addr", p.addr, "error", err, "retry_in", "5s")
					time.Sleep(5 * time.Second)
					continue
				}

				<-p.waitDisconnect()

				if !p.reconnect {
					return
				}

				slog.Info("Disconnected, reconnecting", "addr", p.addr, "retry_in", "5s")
				time.Sleep(5 * time.Second)
			}
		}
	}()
}

func (p *Peer) waitDisconnect() <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		for {
			p.mu.RLock()
			connected := p.connected
			p.mu.RUnlock()

			if !connected {
				close(ch)
				return
			}
			time.Sleep(1 * time.Second)
		}
	}()
	return ch
}

func (p *Peer) Close() {
	select {
	case <-p.done:
		return
	default:
		close(p.done)
	}

	p.mu.Lock()
	if p.conn != nil {
		p.conn.Close()
	}
	p.connected = false
	p.mu.Unlock()
}

func (p *Peer) Send(msg *protocol.Message) error {
	p.mu.RLock()
	connected := p.connected
	p.mu.RUnlock()

	if !connected {
		return fmt.Errorf("not connected to peer")
	}

	select {
	case p.outgoing <- msg:
		return nil
	case <-p.done:
		return fmt.Errorf("peer closed")
	default:
		return fmt.Errorf("outgoing channel full")
	}
}

func (p *Peer) Incoming() <-chan *protocol.Message {
	return p.incoming
}

func (p *Peer) IsConnected() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.connected
}

func (p *Peer) Addr() string {
	return p.addr
}

func (p *Peer) readLoop() {
	defer func() {
		p.mu.Lock()
		p.connected = false
		p.mu.Unlock()
	}()

	for {
		select {
		case <-p.done:
			return
		default:
		}

		p.mu.RLock()
		conn := p.conn
		p.mu.RUnlock()

		if conn == nil {
			return
		}

		msg, err := protocol.Decode(conn)
		if err != nil {
			if err != io.EOF {
				slog.Error("Error reading from peer", "addr", p.addr, "error", err)
			}
			return
		}

		if p.onMessage != nil {
			p.onMessage(msg)
		}

		select {
		case p.incoming <- msg:
		case <-p.done:
			return
		default:
		}
	}
}

func (p *Peer) writeLoop() {
	for {
		select {
		case <-p.done:
			return
		case msg := <-p.outgoing:
			p.mu.RLock()
			conn := p.conn
			connected := p.connected
			p.mu.RUnlock()

			if !connected || conn == nil {
				continue
			}

			data, err := protocol.Encode(msg)
			if err != nil {
				slog.Error("Error encoding message", "error", err)
				continue
			}

			if _, err := conn.Write(data); err != nil {
				slog.Error("Error writing to peer", "addr", p.addr, "error", err)
				p.mu.Lock()
				p.connected = false
				p.mu.Unlock()
				return
			}
		}
	}
}
