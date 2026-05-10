package mysqlpool

import (
	"testing"
)

// TestParseMySQLDSN 测试 DSN 格式解析
func TestParseMySQLDSN(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		wantErr   bool
		errPrefix string
	}{
		{
			name:  "Standard URL with password and port",
			input: "mysql://root:root@localhost:3306/mysql",
			want:  "root:root@tcp(localhost:3306)/mysql",
		},
		{
			name:  "URL without port (default 3306)",
			input: "mysql://root:root@localhost/mysql",
			want:  "root:root@tcp(localhost:3306)/mysql",
		},
		{
			name:  "URL without password",
			input: "mysql://root@localhost:3306/mysql",
			want:  "root@tcp(localhost:3306)/mysql",
		},
		{
			name:  "URL with query parameters",
			input: "mysql://root:root@localhost:3306/mysql?charset=utf8mb4&parseTime=true",
			want:  "root:root@tcp(localhost:3306)/mysql?charset=utf8mb4&parseTime=true",
		},
		{
			name:  "URL with encoded characters",
			input: "mysql://user:p%40ss@localhost:3306/mydb",
			want:  "user:p@ss@tcp(localhost:3306)/mydb",
		},
		{
			name:  "Custom host and port",
			input: "mysql://admin:secret@db.example.com:3307/production",
			want:  "admin:secret@tcp(db.example.com:3307)/production",
		},
		{
			name:  "Native DSN passthrough",
			input: "root:root@tcp(localhost:3306)/mysql",
			want:  "root:root@tcp(localhost:3306)/mysql",
		},
		{
			name:  "Native DSN with params passthrough",
			input: "user:pass@tcp(192.168.1.1:3306)/test?charset=utf8",
			want:  "user:pass@tcp(192.168.1.1:3306)/test?charset=utf8",
		},
		{
			name:  "URL without db name",
			input: "mysql://root:root@localhost:3306/",
			want:  "root:root@tcp(localhost:3306)/",
		},
		// Error cases
		{
			name:      "Invalid - missing @",
			input:     "mysql://invalid-dsn",
			wantErr:   true,
			errPrefix: "missing '@' separator",
		},
		{
			name:      "Invalid - missing database",
			input:     "mysql://root:root@localhost:3306",
			wantErr:   true,
			errPrefix: "missing database name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMySQLDSN(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errPrefix)
					return
				}
				if tt.errPrefix != "" && !contains(err.Error(), tt.errPrefix) {
					t.Errorf("expected error containing %q, got %q", tt.errPrefix, err.Error())
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("parseMySQLDSN(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestNewPoolManager_BadDSN 测试无效 DSN 时 New 应返回错误
func TestNewPoolManager_BadDSN(t *testing.T) {
	// DSN 缺少 @ 分隔符，New 应在 ping 前或 ping 时失败
	_, err := New(t.Context(), "mysql://invalid", 10, 5)
	if err == nil {
		t.Error("expected error for invalid DSN, got nil")
	}
}

// TestNewPoolManager_InvalidHost 测试连接不存在的 MySQL 主机
func TestNewPoolManager_InvalidHost(t *testing.T) {
	_, err := New(t.Context(), "mysql://root:root@255.255.255.255:33060/mysql", 10, 5)
	if err == nil {
		t.Error("expected connection error for unreachable host, got nil")
	}
}


// contains 检查字符串 s 是否包含 substr（简单辅助函数）
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
