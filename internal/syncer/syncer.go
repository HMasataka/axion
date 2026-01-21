package syncer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/HMasataka/axion/internal/peer"
	"github.com/HMasataka/axion/internal/protocol"
	"github.com/HMasataka/axion/internal/watcher"
)

type Syncer struct {
	basePath    string
	watcher     *watcher.Watcher
	server      *peer.Server
	clients     []*peer.Peer
	mu          sync.RWMutex
	done        chan struct{}
	ignoreList  []string
	syncingFiles sync.Map
}

func New(basePath string, listenAddr string, peerAddrs []string, ignoreList []string) (*Syncer, error) {
	w, err := watcher.New(basePath, ignoreList)
	if err != nil {
		return nil, err
	}

	s := &Syncer{
		basePath:   basePath,
		watcher:    w,
		server:     peer.NewServer(listenAddr),
		clients:    make([]*peer.Peer, 0, len(peerAddrs)),
		done:       make(chan struct{}),
		ignoreList: ignoreList,
	}

	for _, addr := range peerAddrs {
		client := peer.NewClient(addr)
		client.SetMessageHandler(s.handleMessage)
		s.clients = append(s.clients, client)
	}

	s.server.SetPeerHandler(func(p *peer.Peer) {
		p.SetMessageHandler(s.handleMessage)
	})

	return s, nil
}

func (s *Syncer) Start() error {
	if err := s.server.Start(); err != nil {
		return err
	}

	for _, client := range s.clients {
		client.StartWithReconnect()
	}

	if err := s.watcher.Start(); err != nil {
		return err
	}

	go s.watchEvents()

	go s.initialSync()

	return nil
}

func (s *Syncer) Stop() {
	close(s.done)
	s.watcher.Stop()
	s.server.Stop()
	for _, client := range s.clients {
		client.Close()
	}
}

func (s *Syncer) watchEvents() {
	for {
		select {
		case <-s.done:
			return
		case event := <-s.watcher.Events():
			s.handleLocalChange(event)
		case err := <-s.watcher.Errors():
			log.Printf("Watcher error: %v", err)
		}
	}
}

func (s *Syncer) handleLocalChange(event watcher.Event) {
	if _, syncing := s.syncingFiles.Load(event.RelativePath); syncing {
		return
	}

	log.Printf("Local change detected: %s (%v)", event.RelativePath, event.Op)

	switch event.Op {
	case watcher.OpCreate, watcher.OpWrite:
		payload := &protocol.FileChangePayload{
			RelativePath: event.RelativePath,
			Hash:         event.Hash,
			ModTime:      event.ModTime.UnixNano(),
			Size:         event.Size,
			IsDir:        event.IsDir,
		}
		msg, _ := protocol.NewFileChangeMessage(payload)
		s.broadcast(msg)

	case watcher.OpRemove, watcher.OpRename:
		payload := &protocol.FileDeletePayload{
			RelativePath: event.RelativePath,
			IsDir:        event.IsDir,
		}
		msg, _ := protocol.NewFileDeleteMessage(payload)
		s.broadcast(msg)
	}
}

func (s *Syncer) handleMessage(msg *protocol.Message) {
	switch msg.Type {
	case protocol.TypeFileChange:
		s.handleFileChange(msg.Payload)
	case protocol.TypeFileRequest:
		s.handleFileRequest(msg.Payload)
	case protocol.TypeFileData:
		s.handleFileData(msg.Payload)
	case protocol.TypeFileDelete:
		s.handleFileDelete(msg.Payload)
	case protocol.TypeSyncRequest:
		s.handleSyncRequest(msg.Payload)
	case protocol.TypeSyncResponse:
		s.handleSyncResponse(msg.Payload)
	}
}

func (s *Syncer) handleFileChange(data []byte) {
	payload, err := protocol.ParseFileChangePayload(data)
	if err != nil {
		log.Printf("Error parsing file change payload: %v", err)
		return
	}

	localPath := filepath.Join(s.basePath, filepath.FromSlash(payload.RelativePath))

	if payload.IsDir {
		s.syncingFiles.Store(payload.RelativePath, true)
		os.MkdirAll(localPath, 0755)
		s.syncingFiles.Delete(payload.RelativePath)
		return
	}

	localHash := s.calculateFileHash(localPath)
	if localHash == payload.Hash {
		return
	}

	localInfo, err := os.Stat(localPath)
	if err == nil {
		localModTime := localInfo.ModTime().UnixNano()
		if localModTime > payload.ModTime {
			log.Printf("Local file %s is newer, skipping remote change", payload.RelativePath)
			return
		}
	}

	log.Printf("Requesting file: %s", payload.RelativePath)
	reqPayload := &protocol.FileRequestPayload{RelativePath: payload.RelativePath}
	msg, _ := protocol.NewFileRequestMessage(reqPayload)
	s.broadcast(msg)
}

func (s *Syncer) handleFileRequest(data []byte) {
	payload, err := protocol.ParseFileRequestPayload(data)
	if err != nil {
		log.Printf("Error parsing file request payload: %v", err)
		return
	}

	localPath := filepath.Join(s.basePath, filepath.FromSlash(payload.RelativePath))

	fileData, err := os.ReadFile(localPath)
	if err != nil {
		log.Printf("Error reading file %s: %v", localPath, err)
		return
	}

	info, err := os.Stat(localPath)
	if err != nil {
		return
	}

	log.Printf("Sending file: %s", payload.RelativePath)
	respPayload := &protocol.FileDataPayload{
		RelativePath: payload.RelativePath,
		Data:         fileData,
		ModTime:      info.ModTime().UnixNano(),
	}
	msg, _ := protocol.NewFileDataMessage(respPayload)
	s.broadcast(msg)
}

func (s *Syncer) handleFileData(data []byte) {
	payload, err := protocol.ParseFileDataPayload(data)
	if err != nil {
		log.Printf("Error parsing file data payload: %v", err)
		return
	}

	localPath := filepath.Join(s.basePath, filepath.FromSlash(payload.RelativePath))

	s.syncingFiles.Store(payload.RelativePath, true)
	defer s.syncingFiles.Delete(payload.RelativePath)

	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("Error creating directory %s: %v", dir, err)
		return
	}

	if err := os.WriteFile(localPath, payload.Data, 0644); err != nil {
		log.Printf("Error writing file %s: %v", localPath, err)
		return
	}

	modTime := time.Unix(0, payload.ModTime)
	os.Chtimes(localPath, modTime, modTime)

	hash := sha256.Sum256(payload.Data)
	s.watcher.SetFileHash(payload.RelativePath, hex.EncodeToString(hash[:]))

	log.Printf("File synced: %s", payload.RelativePath)
}

func (s *Syncer) handleFileDelete(data []byte) {
	payload, err := protocol.ParseFileDeletePayload(data)
	if err != nil {
		log.Printf("Error parsing file delete payload: %v", err)
		return
	}

	localPath := filepath.Join(s.basePath, filepath.FromSlash(payload.RelativePath))

	s.syncingFiles.Store(payload.RelativePath, true)
	defer s.syncingFiles.Delete(payload.RelativePath)

	if payload.IsDir {
		os.RemoveAll(localPath)
	} else {
		os.Remove(localPath)
	}

	log.Printf("File deleted: %s", payload.RelativePath)
}

func (s *Syncer) handleSyncRequest(data []byte) {
	payload, err := protocol.ParseSyncRequestPayload(data)
	if err != nil {
		log.Printf("Error parsing sync request payload: %v", err)
		return
	}

	remoteFiles := make(map[string]protocol.FileInfo)
	for _, f := range payload.Files {
		remoteFiles[f.RelativePath] = f
	}

	localFiles, _ := s.scanLocalFiles()
	localFileMap := make(map[string]protocol.FileInfo)
	for _, f := range localFiles {
		localFileMap[f.RelativePath] = f
	}

	var needFiles []string
	var deleteFiles []string

	for path, remoteFile := range remoteFiles {
		localFile, exists := localFileMap[path]
		if !exists {
			needFiles = append(needFiles, path)
		} else if remoteFile.Hash != localFile.Hash && remoteFile.ModTime > localFile.ModTime {
			needFiles = append(needFiles, path)
		}
	}

	for path := range localFileMap {
		if _, exists := remoteFiles[path]; !exists {
			deleteFiles = append(deleteFiles, path)
		}
	}

	respPayload := &protocol.SyncResponsePayload{
		NeedFiles:   needFiles,
		DeleteFiles: deleteFiles,
	}
	msg, _ := protocol.NewSyncResponseMessage(respPayload)
	s.broadcast(msg)
}

func (s *Syncer) handleSyncResponse(data []byte) {
	payload, err := protocol.ParseSyncResponsePayload(data)
	if err != nil {
		log.Printf("Error parsing sync response payload: %v", err)
		return
	}

	for _, path := range payload.NeedFiles {
		reqPayload := &protocol.FileRequestPayload{RelativePath: path}
		msg, _ := protocol.NewFileRequestMessage(reqPayload)
		s.broadcast(msg)
	}
}

func (s *Syncer) initialSync() {
	time.Sleep(3 * time.Second)

	files, err := s.scanLocalFiles()
	if err != nil {
		log.Printf("Error scanning local files: %v", err)
		return
	}

	payload := &protocol.SyncRequestPayload{Files: files}
	msg, _ := protocol.NewSyncRequestMessage(payload)
	s.broadcast(msg)

	log.Printf("Initial sync request sent with %d files", len(files))
}

func (s *Syncer) scanLocalFiles() ([]protocol.FileInfo, error) {
	var files []protocol.FileInfo

	err := filepath.Walk(s.basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(s.basePath, path)
		if err != nil {
			return err
		}

		if relPath == "." {
			return nil
		}

		for _, pattern := range s.ignoreList {
			if matched, _ := filepath.Match(pattern, filepath.Base(relPath)); matched {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		fileInfo := protocol.FileInfo{
			RelativePath: filepath.ToSlash(relPath),
			ModTime:      info.ModTime().UnixNano(),
			Size:         info.Size(),
			IsDir:        info.IsDir(),
		}

		if !info.IsDir() {
			hash := s.calculateFileHash(path)
			fileInfo.Hash = hash
		}

		files = append(files, fileInfo)
		return nil
	})

	return files, err
}

func (s *Syncer) calculateFileHash(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}

	return hex.EncodeToString(h.Sum(nil))
}

func (s *Syncer) broadcast(msg *protocol.Message) {
	for _, client := range s.clients {
		if client.IsConnected() {
			client.Send(msg)
		}
	}

	s.server.Broadcast(msg)
}

func (s *Syncer) GetStatus() map[string]interface{} {
	status := make(map[string]interface{})

	connectedClients := 0
	for _, client := range s.clients {
		if client.IsConnected() {
			connectedClients++
		}
	}

	serverPeers := len(s.server.GetPeers())

	status["connected_clients"] = connectedClients
	status["server_peers"] = serverPeers
	status["base_path"] = s.basePath

	files, _ := s.scanLocalFiles()
	status["local_files"] = len(files)

	return status
}

func (s *Syncer) GetStatusJSON() string {
	status := s.GetStatus()
	data, _ := json.MarshalIndent(status, "", "  ")
	return string(data)
}
