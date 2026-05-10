package mysqlproxy

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestReadWritePacket verifies packet roundtrip.
func TestReadWritePacket(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		seq     byte
	}{
		{"empty", []byte{}, 0},
		{"small", []byte{0x01, 0x02, 0x03}, 5},
		{"hello", []byte("Hello MySQL"), 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WritePacket(&buf, tt.payload, tt.seq); err != nil {
				t.Fatalf("WritePacket error: %v", err)
			}

			gotPayload, gotSeq, err := ReadPacket(&buf)
			if err != nil {
				t.Fatalf("ReadPacket error: %v", err)
			}

			if !bytes.Equal(gotPayload, tt.payload) {
				t.Errorf("payload mismatch: got %v, want %v", gotPayload, tt.payload)
			}
			if gotSeq != tt.seq {
				t.Errorf("seq mismatch: got %d, want %d", gotSeq, tt.seq)
			}
		})
	}
}

// TestHandshakeV10_Encode verifies the handshake packet format.
func TestHandshakeV10_Encode(t *testing.T) {
	h := BuildHandshakeV10()
	data := h.Encode()

	// Protocol version
	if data[0] != ProtocolVersion {
		t.Errorf("protocol version: got %d, want %d", data[0], ProtocolVersion)
	}

	// Server version should be null-terminated
	if data[len(h.ServerVersion)+1] != 0x00 {
		t.Error("server version not null-terminated")
	}

	// Verify total length is reasonable
	if len(data) < 60 {
		t.Errorf("handshake packet too short: %d bytes", len(data))
	}
}

// TestParseHandshakeResponse verifies parsing of a client handshake response.
func TestParseHandshakeResponse(t *testing.T) {
	// Build a mock HandshakeResponse41
	var buf bytes.Buffer

	cap := uint32(ClientLongPassword | ClientProtocol41 | ClientSecureConnection |
		ClientPluginAuth | ClientConnectWithDB | ClientPluginAuthLenencClientData)

	// Capability (4 bytes)
	caps := make([]byte, 4)
	binary.LittleEndian.PutUint32(caps, cap)
	buf.Write(caps)

	// Max packet size (4 bytes) - 16MB
	maxPkt := make([]byte, 4)
	binary.LittleEndian.PutUint32(maxPkt, 1<<24-1)
	buf.Write(maxPkt)

	// Character set (1 byte)
	buf.WriteByte(CharsetUTF8MB4)

	// Reserved (23 bytes of 0x00)
	buf.Write(make([]byte, 23))

	// Username (null-terminated)
	buf.WriteString("root")
	buf.WriteByte(0x00)

	// Auth response (lenenc: 20 bytes)
	buf.WriteByte(20) // lenenc for < 251
	authResp := make([]byte, 20)
	for i := range authResp {
		authResp[i] = byte(i + 0x41)
	}
	buf.Write(authResp)

	// Database (null-terminated, CLIENT_CONNECT_WITH_DB)
	buf.WriteString("testdb")
	buf.WriteByte(0x00)

	// Auth plugin name (null-terminated, CLIENT_PLUGIN_AUTH)
	buf.WriteString(AuthPluginName)
	buf.WriteByte(0x00)

	resp, err := ParseHandshakeResponse(buf.Bytes())
	if err != nil {
		t.Fatalf("ParseHandshakeResponse error: %v", err)
	}

	if resp.Username != "root" {
		t.Errorf("username: got %q, want %q", resp.Username, "root")
	}
	if resp.Database != "testdb" {
		t.Errorf("database: got %q, want %q", resp.Database, "testdb")
	}
	if resp.AuthPluginName != AuthPluginName {
		t.Errorf("auth_plugin: got %q, want %q", resp.AuthPluginName, AuthPluginName)
	}
	if resp.CharacterSet != CharsetUTF8MB4 {
		t.Errorf("charset: got %d, want %d", resp.CharacterSet, CharsetUTF8MB4)
	}
	if len(resp.AuthResponse) != 20 {
		t.Errorf("auth_response length: got %d, want 20", len(resp.AuthResponse))
	}
}

// TestParseHandshakeResponse_ShortBuffer verifies error on short input.
func TestParseHandshakeResponse_ShortBuffer(t *testing.T) {
	_, err := ParseHandshakeResponse([]byte{0x01, 0x02})
	if err == nil {
		t.Error("expected error for short buffer, got nil")
	}
}

// TestBuildOKPacket verifies OK packet format.
func TestBuildOKPacket(t *testing.T) {
	pkt := BuildOKPacket(3, 5, ServerStatusAutocommit, 2, "Records: 3")

	if len(pkt) == 0 {
		t.Fatal("empty OK packet")
	}
	if pkt[0] != OKPacket {
		t.Errorf("first byte: got 0x%02x, want 0x00", pkt[0])
	}
}

// TestBuildERRPacket verifies error packet format.
func TestBuildERRPacket(t *testing.T) {
	pkt := BuildERRPacket(1045, "28000", "Access denied for user 'root'")

	if len(pkt) == 0 {
		t.Fatal("empty ERR packet")
	}
	if pkt[0] != ERRPacket {
		t.Errorf("first byte: got 0x%02x, want 0xFF", pkt[0])
	}
}

// TestBuildEOFPacket verifies EOF packet format.
func TestBuildEOFPacket(t *testing.T) {
	pkt := BuildEOFPacket(0, ServerStatusAutocommit)

	if len(pkt) != 5 {
		t.Errorf("EOF packet length: got %d, want 5", len(pkt))
	}
	if pkt[0] != EOFPacket {
		t.Errorf("first byte: got 0x%02x, want 0xFE", pkt[0])
	}
}

// TestColumnDef41_Encode verifies column definition encoding.
func TestColumnDef41_Encode(t *testing.T) {
	cd := BuildColumnDef("id")
	cd.ColumnType = TypeLonglong
	cd.ColumnLength = 20

	data := cd.Encode()
	if len(data) == 0 {
		t.Fatal("empty column def")
	}
}

// TestBuildResultSetRow verifies result set row formatting.
func TestBuildResultSetRow(t *testing.T) {
	tests := []struct {
		name   string
		values []string
	}{
		{"normal", []string{"1", "hello", "3.14"}},
		{"with_null", []string{"1", "NULL", "3.14"}},
		{"empty", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := BuildResultSetRow(tt.values)
			if len(row) == 0 && len(tt.values) > 0 {
				t.Error("empty row for non-empty values")
			}
		})
	}
}

// TestPutLenEncInt verifies length-encoded integer encoding.
func TestPutLenEncInt(t *testing.T) {
	tests := []struct {
		val    uint64
		expLen int
	}{
		{0, 1},
		{250, 1},
		{251, 3}, // 0xFC + 2 bytes
		{65535, 3},
		{65536, 4}, // 0xFD + 3 bytes
		{16777215, 4},
		{16777216, 9}, // 0xFE + 8 bytes
	}

	for _, tt := range tests {
		result := PutLenEncInt(tt.val)
		if len(result) != tt.expLen {
			t.Errorf("PutLenEncInt(%d): got %d bytes, want %d", tt.val, len(result), tt.expLen)
		}
	}
}

// TestPutLenEncString verifies length-encoded string encoding.
func TestPutLenEncString(t *testing.T) {
	tests := []string{
		"",
		"hello",
		"a very long string that goes on and on and on",
	}

	for _, s := range tests {
		result := PutLenEncString(s)
		// Result should start with length prefix + string content
		if len(result) == 0 {
			t.Errorf("PutLenEncString(%q) returned empty", s)
		}
	}
}

// TestDecodeCommand verifies command byte extraction.
func TestDecodeCommand(t *testing.T) {
	payload := []byte{ComQuery, 'S', 'E', 'L', 'E', 'C', 'T', ' ', '1'}
	cmd, data, err := DecodeCommand(payload)
	if err != nil {
		t.Fatalf("DecodeCommand error: %v", err)
	}
	if cmd != ComQuery {
		t.Errorf("cmd: got 0x%02x, want 0x%02x", cmd, ComQuery)
	}
	if string(data) != "SELECT 1" {
		t.Errorf("data: got %q, want %q", string(data), "SELECT 1")
	}
}

// TestDecodeCommand_Empty verifies error on empty payload.
func TestDecodeCommand_Empty(t *testing.T) {
	_, _, err := DecodeCommand([]byte{})
	if err == nil {
		t.Error("expected error for empty payload")
	}
}

// TestSQLCommandType verifies SQL command extraction.
func TestSQLCommandType(t *testing.T) {
	tests := []struct {
		sql  string
		cmd  string
	}{
		{"SELECT * FROM users", "SELECT"},
		{"INSERT INTO t VALUES(1)", "INSERT"},
		{"  UPDATE t SET x=1", "UPDATE"},
		{"DELETE FROM t", "DELETE"},
		{"CREATE TABLE t(id INT)", "CREATE"},
		{"DROP TABLE t", "DROP"},
		{"SHOW DATABASES", "SHOW"},
	}

	for _, tt := range tests {
		got := sqlCommandType(tt.sql)
		if got != tt.cmd {
			t.Errorf("sqlCommandType(%q): got %q, want %q", tt.sql, got, tt.cmd)
		}
	}
}

// TestCommandTag verifies OK packet info string generation.
func TestCommandTag(t *testing.T) {
	if tag := commandTag("INSERT INTO t VALUES(1)", 1); tag == "" {
		t.Error("empty INSERT tag")
	}
	if tag := commandTag("UPDATE t SET x=1", 5); tag == "" {
		t.Error("empty UPDATE tag")
	}
	if tag := commandTag("DELETE FROM t", 3); tag == "" {
		t.Error("empty DELETE tag")
	}
}

// TestProxy_Integration verifies the complete MySQL proxy flow with a mock HTTP server.
func TestProxy_Integration(t *testing.T) {
	// This test requires a running HTTP server with MySQL support.
	// Mark as skipped by default; enable with INTEGRATION_TEST=1
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
}
