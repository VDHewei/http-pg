package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Config 统一配置结构体，供 server 和 proxy 共用
type Config struct {
	// ServerAddr HTTP 服务器监听地址（如 ":8080"）
	ServerAddr string `json:"server_addr"`
	// ProxyAddr PgSQL 代理监听地址（如 ":5432"）
	ProxyAddr string `json:"proxy_addr"`
	// MySQLProxyAddr MySQL 代理监听地址（如 ":3306"，阶段五启用）
	MySQLProxyAddr string `json:"mysql_proxy_addr"`
	// PostgresDSN PgSQL 数据源连接串
	PostgresDSN string `json:"postgres_dsn"`
	// MySQLDSN MySQL 数据源连接串（阶段五启用）
	MySQLDSN string `json:"mysql_dsn"`
	// EncryptionKey AES-256-GCM 加密密钥
	EncryptionKey string `json:"encryption_key"`
	// MaxConnections 最大数据库连接数
	MaxConnections int32 `json:"max_connections"`
	// MinConnections 最小数据库连接数
	MinConnections int32 `json:"min_connections"`
	// ServerURL HTTP 服务器完整 URL（如 "http://192.168.1.100:8080"）
	// 代理使用此字段连接后端 HTTP 服务器
	ServerURL string `json:"server_url"`
	// ProxyProtocol 代理协议类型: pg / mysql / both
	// 控制启动哪些数据库协议的代理
	ProxyProtocol string `json:"proxy_protocol"`
}

// DefaultConfig 返回带默认值的配置实例
func DefaultConfig() *Config {
	return &Config{
		ServerAddr:      ":8080",
		ProxyAddr:       ":5432",
		MySQLProxyAddr:  ":3306",
		PostgresDSN:     "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable",
		MySQLDSN:        "mysql://root:root@localhost:3306/mysql",
		EncryptionKey:   "",
		MaxConnections:  20,
		MinConnections:  5,
		ServerURL:       "",
		ProxyProtocol:   "pg",
	}
}

// GetServerURL 获取服务端完整 URL
// 优先使用 ServerURL 字段，为空时自动拼接 "http://localhost" + ServerAddr
func (c *Config) GetServerURL() string {
	if c.ServerURL != "" {
		return strings.TrimRight(c.ServerURL, "/")
	}
	return "http://localhost" + c.ServerAddr
}

// Validate 验证配置有效性
func (c *Config) Validate() error {
	if c.EncryptionKey == "" {
		return fmt.Errorf("encryption key is required (set ENCRYPTION_KEY env or encryption_key in config)")
	}
	if c.ProxyProtocol != "" && c.ProxyProtocol != "pg" && c.ProxyProtocol != "mysql" && c.ProxyProtocol != "both" {
		return fmt.Errorf("invalid proxy_protocol: %s (valid values: pg, mysql, both)", c.ProxyProtocol)
	}
	return nil
}

// LoadConfig 从文件加载配置，文件不存在时使用默认值
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 配置文件不存在时从环境变量补充密钥
			cfg.EncryptionKey = os.Getenv("ENCRYPTION_KEY")
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// JSON 中的密钥为空时从环境变量补充
	if cfg.EncryptionKey == "" {
		cfg.EncryptionKey = os.Getenv("ENCRYPTION_KEY")
	}

	return cfg, nil
}
