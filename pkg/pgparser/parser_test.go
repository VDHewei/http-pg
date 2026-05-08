package pgparser

import (
	"testing"
)

func TestExtractSQL_Query(t *testing.T) {
	// Simulate a Query message payload: "SELECT 1;\0"
	payload := append([]byte("SELECT 1;"), 0)
	sql := ExtractSQL('Q', payload)
	if sql != "SELECT 1;" {
		t.Errorf("expected 'SELECT 1;', got %q", sql)
	}
}

func TestExtractSQL_Parse(t *testing.T) {
	// Simulate a Parse message: "\0SELECT * FROM users;\0\0\0"
	payload := []byte{}
	payload = append(payload, 0) // empty statement name
	payload = append(payload, []byte("SELECT * FROM users;")...)
	payload = append(payload, 0) // null terminator
	payload = append(payload, 0, 0) // 0 param types

	sql := ExtractSQL('P', payload)
	if sql != "SELECT * FROM users;" {
		t.Errorf("expected 'SELECT * FROM users;', got %q", sql)
	}
}

func TestMessageTypeName(t *testing.T) {
	tests := []struct {
		msgType byte
		want    string
	}{
		{0, "StartupMessage"},
		{'Q', "Query"},
		{'P', "Parse"},
		{'X', "Terminate"},
		{'R', "Authentication"},
		{'Z', "ReadyForQuery"},
		{'T', "RowDescription"},
		{'C', "CommandComplete"},
	}

	for _, tt := range tests {
		got := MessageTypeName(tt.msgType)
		if got != tt.want {
			t.Errorf("MessageTypeName(%c) = %q, want %q", tt.msgType, got, tt.want)
		}
	}
}

func TestIsQueryType(t *testing.T) {
	if !IsQueryType('Q') {
		t.Error("Q should be a query type")
	}
	if !IsQueryType('P') {
		t.Error("P should be a query type")
	}
	if IsQueryType('X') {
		t.Error("X should not be a query type")
	}
}

func TestIsTerminate(t *testing.T) {
	if !IsTerminate('X') {
		t.Error("X should be terminate")
	}
	if IsTerminate('Q') {
		t.Error("Q should not be terminate")
	}
}

func TestMessageEncodeDecode(t *testing.T) {
	m := &Message{
		Type:    'Q',
		Payload: []byte("SELECT 1;\x00"),
	}

	encoded := m.Encode()
	decoded, err := DecodeMessage(encoded)
	if err != nil {
		t.Fatalf("DecodeMessage failed: %v", err)
	}

	if decoded.Type != 'Q' {
		t.Errorf("Type mismatch: got %c, want Q", decoded.Type)
	}
	if string(trimNull(decoded.Payload)) != "SELECT 1;" {
		t.Errorf("Payload mismatch: got %q", decoded.Payload)
	}
}

func TestParseStartupMessage(t *testing.T) {
	// Build a startup message packet
	params := "user\x00postgres\x00database\x00testdb\x00\x00"
	raw := make([]byte, 8+len(params))
	// Length (including itself) = 8 + len(params)
	raw[0] = 0
	raw[1] = 0
	raw[2] = byte((8 + len(params)) >> 8)
	raw[3] = byte(8 + len(params))
	// Protocol version 3.0
	raw[4] = 0
	raw[5] = 3
	raw[6] = 0
	raw[7] = 0
	copy(raw[8:], []byte(params))

	data, err := ParseStartupMessage(raw)
	if err != nil {
		t.Fatalf("ParseStartupMessage failed: %v", err)
	}

	if data.Parameters["user"] != "postgres" {
		t.Errorf("user mismatch: got %q", data.Parameters["user"])
	}
	if data.Parameters["database"] != "testdb" {
		t.Errorf("database mismatch: got %q", data.Parameters["database"])
	}
}

func TestDecodeMessageShort(t *testing.T) {
	_, err := DecodeMessage([]byte{0, 0, 0})
	if err == nil {
		t.Error("expected error for short message, got nil")
	}
}

func TestDecodeMessageTruncated(t *testing.T) {
	data := []byte{'Q', 0, 0, 0, 10} // claims 10 byte payload
	_, err := DecodeMessage(data)
	if err == nil {
		t.Error("expected error for truncated message, got nil")
	}
}
