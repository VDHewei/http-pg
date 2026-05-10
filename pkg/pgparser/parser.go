package pgparser

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Message wraps a PgSQL wire protocol message with type identification.
// Type 0 indicates a StartupMessage.
type Message struct {
	Type    byte   // message type byte (0 for StartupMessage)
	Payload []byte // raw message bytes (for regular msgs: type+length+body; for Startup: full packet)
}

// Encode serializes a Message to binary format for transport.
// Format: [1 byte type] [4 bytes length] [payload]
func (m *Message) Encode() []byte {
	buf := make([]byte, 5+len(m.Payload))
	buf[0] = m.Type
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(m.Payload)))
	copy(buf[5:], m.Payload)
	return buf
}

// DecodeMessage decodes a Message from binary transport format.
func DecodeMessage(data []byte) (*Message, error) {
	if len(data) < 5 {
		return nil, fmt.Errorf("message too short: %d bytes", len(data))
	}
	payloadLen := int(binary.BigEndian.Uint32(data[1:5]))
	if len(data) < 5+payloadLen {
		return nil, fmt.Errorf("truncated message: expected %d, got %d", 5+payloadLen, len(data))
	}
	return &Message{
		Type:    data[0],
		Payload: makeCopy(data[5 : 5+payloadLen]),
	}, nil
}

// StartupMessageData extracts parameters from a StartupMessage payload.
type StartupMessageData struct {
	ProtocolVersion uint32
	Parameters      map[string]string
}

// ParseStartupMessage parses a raw startup message buffer.
func ParseStartupMessage(raw []byte) (*StartupMessageData, error) {
	if len(raw) < 8 {
		return nil, fmt.Errorf("startup message too short: %d", len(raw))
	}

	// Format: [4 bytes length] [4 bytes protocol version] [key\0value\0...] [\0]
	totalLen := int32(binary.BigEndian.Uint32(raw[0:4]))
	_ = totalLen

	protoVer := binary.BigEndian.Uint32(raw[4:8])

	data := &StartupMessageData{
		ProtocolVersion: protoVer,
		Parameters:      make(map[string]string),
	}

	// Parse key-value pairs
	buf := raw[8:]
	for i := 0; i < len(buf); {
		// Find key (null-terminated)
		keyEnd := findNull(buf, i)
		if keyEnd < 0 {
			break
		}
		key := string(buf[i:keyEnd])

		// Find value (null-terminated)
		valEnd := findNull(buf, keyEnd+1)
		if valEnd < 0 {
			break
		}
		val := string(buf[keyEnd+1 : valEnd])

		data.Parameters[key] = val
		i = valEnd + 1
	}

	return data, nil
}

// ReadStartupRaw reads the raw startup message bytes from a reader.
// Returns the full packet including length prefix.
func ReadStartupRaw(r io.Reader) ([]byte, error) {
	// Read first 4 bytes to get total length
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return nil, err
	}

	totalLen := int32(binary.BigEndian.Uint32(lenBuf))
	if totalLen < 4 {
		return nil, fmt.Errorf("invalid startup packet length: %d", totalLen)
	}

	// Read remaining bytes
	remaining := totalLen - 4
	restBuf := make([]byte, remaining)
	if _, err := io.ReadFull(r, restBuf); err != nil {
		return nil, err
	}

	// Build full packet
	fullPacket := make([]byte, 4+remaining)
	copy(fullPacket, lenBuf)
	copy(fullPacket[4:], restBuf)

	return fullPacket, nil
}

// ExtractSQL extracts the SQL query string from a Query message payload.
// The payload for a Query message is: [query_string\0]
func ExtractSQL(msgType byte, payload []byte) string {
	switch msgType {
	case 'Q': // Query (simple query)
		return string(trimNull(payload))
	case 'P': // Parse (extended query)
		// Parse message: [statement_name\0] [query_string\0] [num_params (2 bytes)] [param_types (4 bytes each)]
		parts := splitNull(payload)
		if len(parts) >= 2 {
			return parts[1]
		}
	}
	return ""
}

// ExtractStatementName extracts the statement name from a Parse message payload.
func ExtractStatementName(payload []byte) string {
	parts := splitNull(payload)
	if len(parts) >= 1 {
		return parts[0]
	}
	return ""
}

// IsQueryType returns true if the message type is a query-related message.
func IsQueryType(msgType byte) bool {
	switch msgType {
	case 'Q', 'P', 'B', 'E', 'D', 'H', 'S':
		return true
	}
	return false
}

// IsTerminate returns true if the message is a Terminate message.
func IsTerminate(msgType byte) bool {
	return msgType == 'X'
}

// MessageTypeName returns a human-readable name for the message type.
func MessageTypeName(msgType byte) string {
	switch msgType {
	case 0:
		return "StartupMessage"
	case 'Q':
		return "Query"
	case 'P':
		return "Parse"
	case 'B':
		return "Bind"
	case 'E':
		return "Execute"
	case 'D':
		return "Describe"
	case 'H':
		return "Flush"
	case 'S':
		return "Sync"
	case 'X':
		return "Terminate"
	case 'p':
		return "PasswordMessage"
	case 'R':
		return "Authentication"
	case 'K':
		return "BackendKeyData"
	case 'Z':
		return "ReadyForQuery"
	case 'T':
		return "RowDescription"
	case 'C':
		return "CommandComplete"
	case 'N':
		return "NoticeResponse"
	default:
		return fmt.Sprintf("Unknown(0x%02x)", msgType)
	}
}

func findNull(buf []byte, start int) int {
	for i := start; i < len(buf); i++ {
		if buf[i] == 0 {
			return i
		}
	}
	return -1
}

func trimNull(b []byte) []byte {
	for i, c := range b {
		if c == 0 {
			return b[:i]
		}
	}
	return b
}

func splitNull(b []byte) []string {
	var parts []string
	start := 0
	for i, c := range b {
		if c == 0 {
			parts = append(parts, string(b[start:i]))
			start = i + 1
		}
	}
	if start < len(b) {
		parts = append(parts, string(b[start:]))
	}
	return parts
}

func makeCopy(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
