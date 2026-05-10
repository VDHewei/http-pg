package httpclient

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/http-pg/http-pg/pkg/crypto"
)

// ClientConfig HTTP 客户端配置
type ClientConfig struct {
	// BaseURL 后端 HTTP 服务器的基础 URL（如 "http://192.168.1.100:8080"）
	BaseURL string
	// Timeout HTTP 请求超时时间（默认 30 秒）
	Timeout time.Duration
	// RetryAttempts 请求失败时的重试次数（默认 0，不重试）
	RetryAttempts int
}

// DefaultClientConfig 返回带默认值的客户端配置
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		Timeout:       30 * time.Second,
		RetryAttempts: 0,
	}
}

// Client HTTP 客户端，用于代理与后端 API 服务器间的加密通信
type Client struct {
	// serverURL 后端服务器基础 URL
	serverURL string
	// httpClient 底层 HTTP 客户端
	httpClient *http.Client
	// encKey 已派生的 AES-256-GCM 加密密钥（32 字节）
	encKey []byte
	// retryAttempts 重试次数
	retryAttempts int
}

// NewClient 使用默认配置创建 HTTP 客户端（向后兼容）
func NewClient(serverURL, encKey string) (*Client, error) {
	cfg := DefaultClientConfig()
	cfg.BaseURL = serverURL
	return NewClientWithConfig(cfg, encKey)
}

// NewClientWithConfig 使用自定义配置创建 HTTP 客户端
func NewClientWithConfig(cfg ClientConfig, encKey string) (*Client, error) {
	key, err := crypto.DeriveKey(encKey)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}

	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	return &Client{
		serverURL:     cfg.BaseURL,
		httpClient:    &http.Client{Timeout: cfg.Timeout},
		encKey:        key,
		retryAttempts: cfg.RetryAttempts,
	}, nil
}

// SessionRequest 在服务器上创建新的数据库会话
//
// 处理流程:
//  1. 加密启动参数
//  2. POST /api/v1/session
//  3. 返回服务端生成的 session UUID
func (c *Client) SessionRequest(params []byte) (string, error) {
	// 加密启动参数
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
		return "", fmt.Errorf("session creation failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

// QueryRequest 发送 SQL 查询请求并返回解密后的响应
//
// 处理流程:
//  1. 加密消息体
//  2. POST /api/v1/query（附带 X-Session-ID 头）
//  3. 解密响应体
func (c *Client) QueryRequest(sessionID string, msgBytes []byte) ([]byte, error) {
	// 加密消息
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
		return nil, fmt.Errorf("query failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	// 解密响应
	decrypted, err := crypto.Decrypt(body, c.encKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt response: %w", err)
	}

	return decrypted, nil
}

// CloseSession 通知服务器关闭指定会话
//
// DELETE /api/v1/session/:session_id
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
