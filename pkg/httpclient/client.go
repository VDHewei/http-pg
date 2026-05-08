package httpclient

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/http-pg/http-pg/pkg/crypto"
)

// Client is an HTTP client for forwarding PgSQL messages to the API server.
type Client struct {
	serverURL  string
	httpClient *http.Client
	encKey     []byte
}

// Request represents a proxied PgSQL message.
type Request struct {
	SessionID string `json:"session_id"`
	Message   []byte `json:"message"`
}

// Response represents a response from the API server.
type Response struct {
	SessionID string `json:"session_id"`
	Result    []byte `json:"result"`
	Error     string `json:"error,omitempty"`
}

// NewClient creates a new HTTP client.
func NewClient(serverURL, encKey string) (*Client, error) {
	key, err := crypto.DeriveKey(encKey)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}

	return &Client{
		serverURL: serverURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		encKey: key,
	}, nil
}

// SessionRequest creates a new session on the server.
func (c *Client) SessionRequest(params []byte) (string, error) {
	// Encrypt the startup parameters
	encrypted, err := crypto.Encrypt(params, c.encKey)
	if err != nil {
		return "", fmt.Errorf("encrypt startup params: %w", err)
	}

	reqBody := bytes.NewBuffer(encrypted)
	resp, err := c.httpClient.Post(c.serverURL+"/api/v1/session", "application/octet-stream", reqBody)
	if err != nil {
		return "", fmt.Errorf("http post session: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read session response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("session creation failed: status=%d, body=%s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

// QueryRequest sends a PgSQL message and returns the response.
func (c *Client) QueryRequest(sessionID string, msgBytes []byte) ([]byte, error) {
	// Encrypt the message
	encrypted, err := crypto.Encrypt(msgBytes, c.encKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt message: %w", err)
	}

	req, err := http.NewRequest("POST", c.serverURL+"/api/v1/query", bytes.NewBuffer(encrypted))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Session-ID", sessionID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http post query: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read query response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query failed: status=%d, body=%s", resp.StatusCode, string(body))
	}

	// Decrypt the response
	decrypted, err := crypto.Decrypt(body, c.encKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt response: %w", err)
	}

	return decrypted, nil
}

// CloseSession notifies the server to close a session.
func (c *Client) CloseSession(sessionID string) error {
	req, err := http.NewRequest("DELETE", c.serverURL+"/api/v1/session/"+sessionID, nil)
	if err != nil {
		return fmt.Errorf("create close request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http delete session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("close session failed: status=%d", resp.StatusCode)
	}

	return nil
}
