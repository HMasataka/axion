package peer

import (
	"log"
	"net"
	"sync"

	"github.com/HMasataka/axion/internal/protocol"
)

type Server struct {
	addr     string
	listener net.Listener
	peers    map[string]*Peer
	mu       sync.RWMutex
	done     chan struct{}
	onPeer   func(*Peer)
}

func NewServer(addr string) *Server {
	return &Server{
		addr:  addr,
		peers: make(map[string]*Peer),
		done:  make(chan struct{}),
	}
}

func (s *Server) SetPeerHandler(handler func(*Peer)) {
	s.onPeer = handler
}

func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}

	s.listener = listener
	log.Printf("Server listening on %s", s.addr)

	go s.acceptLoop()

	return nil
}

func (s *Server) Stop() {
	select {
	case <-s.done:
		return
	default:
		close(s.done)
	}

	if s.listener != nil {
		s.listener.Close()
	}

	s.mu.Lock()
	for _, peer := range s.peers {
		peer.Close()
	}
	s.mu.Unlock()
}

func (s *Server) acceptLoop() {
	for {
		select {
		case <-s.done:
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				log.Printf("Error accepting connection: %v", err)
				continue
			}
		}

		peer := NewFromConn(conn)
		addr := conn.RemoteAddr().String()

		s.mu.Lock()
		s.peers[addr] = peer
		s.mu.Unlock()

		log.Printf("New peer connected: %s", addr)

		if s.onPeer != nil {
			s.onPeer(peer)
		}

		peer.Start()
	}
}

func (s *Server) GetPeers() []*Peer {
	s.mu.RLock()
	defer s.mu.RUnlock()

	peers := make([]*Peer, 0, len(s.peers))
	for _, p := range s.peers {
		peers = append(peers, p)
	}
	return peers
}

func (s *Server) RemovePeer(addr string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if peer, exists := s.peers[addr]; exists {
		peer.Close()
		delete(s.peers, addr)
	}
}

func (s *Server) Broadcast(msg *protocol.Message) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, peer := range s.peers {
		if peer.IsConnected() {
			peer.Send(msg)
		}
	}
}
