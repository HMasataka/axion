package transfer

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/HMasataka/axion/internal/clientfs"
)

// Config は HTTP transfer クライアント設定。
type Config struct {
	ServerURL string
	PSK       string
	ClientID  string
	Jail      *clientfs.Jail
	HTTP      *http.Client
}

// Client は upload/download を提供する。
type Client struct {
	cfg     Config
	baseURL *url.URL
	http    *http.Client
}

// New は Client を作る。ServerURL が "ws://" prefix なら "http://" に変換、"wss://" → "https://"。
func New(cfg Config) (*Client, error) {
	serverURL := cfg.ServerURL
	switch {
	case strings.HasPrefix(serverURL, "ws://"):
		serverURL = "http://" + strings.TrimPrefix(serverURL, "ws://")
	case strings.HasPrefix(serverURL, "wss://"):
		serverURL = "https://" + strings.TrimPrefix(serverURL, "wss://")
	}

	base, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("transfer: invalid ServerURL: %w", err)
	}

	httpClient := cfg.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		cfg:     cfg,
		baseURL: base,
		http:    httpClient,
	}, nil
}

func (c *Client) blobURL(sha string) string {
	return c.baseURL.String() + "/v1/blobs/" + sha
}

func (c *Client) setAuthHeaders(req *http.Request) {
	req.Header.Set("X-Axion-Client-ID", c.cfg.ClientID)
	req.Header.Set("Authorization", "Bearer "+c.cfg.PSK)
}
