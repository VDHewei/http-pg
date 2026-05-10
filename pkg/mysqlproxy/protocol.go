package mysqlproxy

import (
	"encoding/binary"
	"fmt"
	"io"
)

// MySQL Protocol Constants
const (
	// Capability flags
	ClientLongPassword              uint32 = 1 << 0
	ClientFoundRows                 uint32 = 1 << 1
	ClientLongFlag                  uint32 = 1 << 2
	ClientConnectWithDB             uint32 = 1 << 3
	ClientProtocol41                uint32 = 1 << 9
	ClientInteractive               uint32 = 1 << 10
	ClientSSL                       uint32 = 1 << 11
	ClientTransactions              uint32 = 1 << 13
	ClientSecureConnection          uint32 = 1 << 15
	ClientMultiStatements           uint32 = 1 << 16
	ClientMultiResults              uint32 = 1 << 17
	ClientPSMultiResults            uint32 = 1 << 18
	ClientPluginAuth                uint32 = 1 << 19
	ClientConnectAttrs              uint32 = 1 << 20
	ClientPluginAuthLenencClientData uint32 = 1 << 21
	ClientDeprecateEOF              uint32 = 1 << 24

	// Server status flags
	ServerStatusAutocommit  uint16 = 0x0002
	ServerStatusInTrans     uint16 = 0x0001
	ServerStatusCursorExists uint16 = 0x0040
	ServerStatusLastRowSent  uint16 = 0x0080

	// Character set: utf8mb4
	CharsetUTF8MB4 byte = 45

	// Auth plugin name
	AuthPluginName = "mysql_native_password"

	// Max packet size (16MB - 1)
	MaxPayloadLen = 1<<24 - 1

	// Protocol version
	ProtocolVersion = 10

	// Packet header size
	PacketHeaderSize = 4

	// Packet type markers
	OKPacket  byte = 0x00
	EOFPacket byte = 0xFE
	ERRPacket byte = 0xFF

	// Column types
	TypeDecimal    byte = 0x00
	TypeTiny       byte = 0x01
	TypeShort      byte = 0x02
	TypeLong       byte = 0x03
	TypeFloat      byte = 0x04
	TypeDouble     byte = 0x05
	TypeNull       byte = 0x06
	TypeTimestamp  byte = 0x07
	TypeLonglong   byte = 0x08
	TypeInt24      byte = 0x09
	TypeDate       byte = 0x0a
	TypeTime       byte = 0x0b
	TypeDatetime   byte = 0x0c
	TypeYear       byte = 0x0d
	TypeVarchar    byte = 0x0f
	TypeJSON       byte = 0xf5
	TypeNewDecimal byte = 0xf6
	TypeEnum       byte = 0xf7
	TypeSet        byte = 0xf8
	TypeTinyBlob   byte = 0xf9
	TypeMediumBlob byte = 0xfa
	TypeLongBlob   byte = 0xfb
	TypeBlob       byte = 0xfc
	TypeVarString  byte = 0xfd
	TypeString     byte = 0xfe

	// Commands
	ComSleep          byte = 0x00
	ComQuit           byte = 0x01
	ComInitDB         byte = 0x02
	ComQuery          byte = 0x03
	ComFieldList      byte = 0x04
	ComCreateDB       byte = 0x05
	ComDropDB         byte = 0x06
	ComRefresh        byte = 0x07
	ComShutdown       byte = 0x08
	ComStatistics     byte = 0x09
	ComProcessInfo    byte = 0x0A
	ComConnect        byte = 0x0B
	ComProcessKill    byte = 0x0C
	ComDebug          byte = 0x0D
	ComPing           byte = 0x0E
	ComTime           byte = 0x0F
	ComDelayedInsert  byte = 0x10
	ComChangeUser     byte = 0x11
	ComStmtPrepare    byte = 0x16
	ComStmtExecute    byte = 0x17
	ComStmtClose      byte = 0x19
	ComStmtReset      byte = 0x1A
	ComSetOption      byte = 0x1B
	ComStmtFetch      byte = 0x1C
)

// ServerCapabilityFlags is the set of capabilities advertised by our proxy server.
var ServerCapabilityFlags = ClientLongPassword |
	ClientFoundRows |
	ClientLongFlag |
	ClientConnectWithDB |
	ClientProtocol41 |
	ClientTransactions |
	ClientSecureConnection |
	ClientMultiStatements |
	ClientMultiResults |
	ClientPluginAuth |
	ClientPluginAuthLenencClientData

// ReadPacket reads a complete MySQL packet from the reader.
// Returns payload bytes and sequence number.
func ReadPacket(r io.Reader) (payload []byte, seq byte, err error) {
	header := make([]byte, PacketHeaderSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, 0, fmt.Errorf("read packet header: %w", err)
	}

	length := int(uint32(header[0]) | uint32(header[1])<<8 | uint32(header[2])<<16)
	seq = header[3]

	payload = make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, 0, fmt.Errorf("read packet payload: %w", err)
		}
	}

	return payload, seq, nil
}

// WritePacket writes a MySQL packet to the writer.
func WritePacket(w io.Writer, payload []byte, seq byte) error {
	length := len(payload)
	if length > MaxPayloadLen {
		return fmt.Errorf("payload too large: %d > %d", length, MaxPayloadLen)
	}

	header := make([]byte, PacketHeaderSize)
	header[0] = byte(length)
	header[1] = byte(length >> 8)
	header[2] = byte(length >> 16)
	header[3] = seq

	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("write packet header: %w", err)
	}
	if length > 0 {
		if _, err := w.Write(payload); err != nil {
			return fmt.Errorf("write packet payload: %w", err)
		}
	}

	return nil
}

// HandshakeV10 represents the initial server greeting packet.
type HandshakeV10 struct {
	ProtocolVersion byte
	ServerVersion   string
	ConnectionID    uint32
	AuthPluginData  []byte
	Capability      uint32
	CharacterSet    byte
	StatusFlags     uint16
	AuthPluginName  string
}

// Encode encodes HandshakeV10 into wire format bytes.
func (h *HandshakeV10) Encode() []byte {
	var buf []byte

	// Protocol version
	buf = append(buf, h.ProtocolVersion)

	// Server version (null-terminated)
	buf = append(buf, []byte(h.ServerVersion)...)
	buf = append(buf, 0x00)

	// Connection ID (4 bytes LE)
	connID := make([]byte, 4)
	binary.LittleEndian.PutUint32(connID, h.ConnectionID)
	buf = append(buf, connID...)

	// Auth plugin data part 1 (8 bytes)
	part1 := h.AuthPluginData
	if len(part1) > 8 {
		part1 = part1[:8]
	}
	buf = append(buf, part1...)
	if len(part1) < 8 {
		buf = append(buf, make([]byte, 8-len(part1))...)
	}

	// Filler (0x00)
	buf = append(buf, 0x00)

	// Capability flags - lower 2 bytes
	capLower := make([]byte, 2)
	binary.LittleEndian.PutUint16(capLower, uint16(h.Capability&0xFFFF))
	buf = append(buf, capLower...)

	// Character set
	buf = append(buf, h.CharacterSet)

	// Status flags (2 bytes)
	status := make([]byte, 2)
	binary.LittleEndian.PutUint16(status, h.StatusFlags)
	buf = append(buf, status...)

	// Capability flags - upper 2 bytes
	capUpper := make([]byte, 2)
	binary.LittleEndian.PutUint16(capUpper, uint16(h.Capability>>16))
	buf = append(buf, capUpper...)

	// Auth plugin data length (includes null terminator)
	buf = append(buf, byte(len(h.AuthPluginData)+1))

	// Reserved (10 bytes of 0x00)
	buf = append(buf, make([]byte, 10)...)

	// Auth plugin data part 2 (12 bytes + null terminator)
	if len(h.AuthPluginData) > 8 {
		part2 := h.AuthPluginData[8:]
		if len(part2) > 12 {
			part2 = part2[:12]
		}
		buf = append(buf, part2...)
		if len(part2) < 12 {
			buf = append(buf, make([]byte, 12-len(part2))...)
		}
	} else {
		buf = append(buf, make([]byte, 12)...)
	}
	buf = append(buf, 0x00)

	// Auth plugin name (null-terminated)
	buf = append(buf, []byte(h.AuthPluginName)...)
	buf = append(buf, 0x00)

	return buf
}

// BuildHandshakeV10 creates a standard HandshakeV10 packet for the proxy.
func BuildHandshakeV10() *HandshakeV10 {
	authData := make([]byte, 20)
	for i := range authData {
		authData[i] = byte(i + 0x21) // starting at '!'
	}
	return &HandshakeV10{
		ProtocolVersion: ProtocolVersion,
		ServerVersion:   "5.7.42-HTTP-PG-MySQL-proxy",
		ConnectionID:    1,
		AuthPluginData:  authData,
		Capability:      ServerCapabilityFlags,
		CharacterSet:    CharsetUTF8MB4,
		StatusFlags:     ServerStatusAutocommit,
		AuthPluginName:  AuthPluginName,
	}
}

// HandshakeResponse41 contains parsed data from a client handshake response.
type HandshakeResponse41 struct {
	Capability     uint32
	MaxPacketSize  uint32
	CharacterSet   byte
	Username       string
	AuthResponse   []byte
	Database       string
	AuthPluginName string
}

// ParseHandshakeResponse parses a client's HandshakeResponse41 from raw bytes.
func ParseHandshakeResponse(payload []byte) (*HandshakeResponse41, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("handshake response too short: %d bytes", len(payload))
	}

	resp := &HandshakeResponse41{}
	pos := 0

	// Capability flags (4 bytes LE)
	resp.Capability = binary.LittleEndian.Uint32(payload[pos : pos+4])
	pos += 4

	// Max packet size (4 bytes LE)
	resp.MaxPacketSize = binary.LittleEndian.Uint32(payload[pos : pos+4])
	pos += 4

	// Character set (1 byte)
	resp.CharacterSet = payload[pos]
	pos++

	// Reserved (23 bytes of 0x00)
	pos += 23

	// Username (null-terminated)
	if pos < len(payload) {
		end := findNull(payload, pos)
		resp.Username = string(payload[pos:end])
		pos = end + 1
	}

	// Auth response length
	authLen := 0
	if (resp.Capability & ClientPluginAuthLenencClientData) != 0 {
		authLen, pos = decodeLenEncInt(payload, pos)
	} else if (resp.Capability & ClientSecureConnection) != 0 {
		if pos < len(payload) {
			authLen = int(payload[pos])
			pos++
		}
	} else {
		if pos < len(payload) {
			end := findNull(payload, pos)
			authLen = end - pos
		}
	}

	// Auth response data
	if authLen > 0 && pos+authLen <= len(payload) {
		resp.AuthResponse = make([]byte, authLen)
		copy(resp.AuthResponse, payload[pos:pos+authLen])
		pos += authLen
	}

	// Database (CLIENT_CONNECT_WITH_DB)
	if (resp.Capability&ClientConnectWithDB) != 0 && pos < len(payload) {
		end := findNull(payload, pos)
		resp.Database = string(payload[pos:end])
		pos = end + 1
	}

	// Auth plugin name (CLIENT_PLUGIN_AUTH)
	if (resp.Capability&ClientPluginAuth) != 0 && pos < len(payload) {
		end := findNull(payload, pos)
		resp.AuthPluginName = string(payload[pos:end])
	}

	return resp, nil
}

// findNull finds the position of the next null byte starting from pos.
func findNull(data []byte, pos int) int {
	for i := pos; i < len(data); i++ {
		if data[i] == 0x00 {
			return i
		}
	}
	return len(data)
}

// decodeLenEncInt decodes a length-encoded integer from data at position pos.
// Returns the decoded value and new position.
func decodeLenEncInt(data []byte, pos int) (int, int) {
	if pos >= len(data) {
		return 0, pos
	}

	switch data[pos] {
	case 0xFB: // NULL
		return 0, pos + 1
	case 0xFC: // 2 bytes
		if pos+3 > len(data) {
			return 0, pos + 1
		}
		return int(binary.LittleEndian.Uint16(data[pos+1 : pos+3])), pos + 3
	case 0xFD: // 3 bytes
		if pos+4 > len(data) {
			return 0, pos + 1
		}
		return int(uint32(data[pos+1]) | uint32(data[pos+2])<<8 | uint32(data[pos+3])<<16), pos + 4
	case 0xFE: // 8 bytes
		if pos+9 > len(data) {
			return 0, pos + 1
		}
		return int(binary.LittleEndian.Uint64(data[pos+1 : pos+9])), pos + 9
	default: // 1 byte (< 251)
		return int(data[pos]), pos + 1
	}
}

// BuildOKPacket builds a MySQL OK packet (header 0x00).
func BuildOKPacket(affectedRows, lastInsertID uint64, statusFlags uint16, warnings uint16, info string) []byte {
	var buf []byte
	buf = append(buf, OKPacket)
	buf = append(buf, PutLenEncInt(affectedRows)...)
	buf = append(buf, PutLenEncInt(lastInsertID)...)

	status := make([]byte, 2)
	binary.LittleEndian.PutUint16(status, statusFlags)
	buf = append(buf, status...)

	warn := make([]byte, 2)
	binary.LittleEndian.PutUint16(warn, warnings)
	buf = append(buf, warn...)

	if info != "" {
		buf = append(buf, PutLenEncString(info)...)
	}

	return buf
}

// BuildERRPacket builds a MySQL error packet (header 0xFF).
func BuildERRPacket(errCode uint16, sqlState, message string) []byte {
	var buf []byte
	buf = append(buf, ERRPacket)

	code := make([]byte, 2)
	binary.LittleEndian.PutUint16(code, errCode)
	buf = append(buf, code...)

	// SQL state marker ('#')
	buf = append(buf, '#')

	// SQL state (5 bytes, padded with space)
	state := sqlState
	if len(state) < 5 {
		state = state + "     "[:5-len(state)]
	} else {
		state = state[:5]
	}
	buf = append(buf, []byte(state)...)

	buf = append(buf, []byte(message)...)

	return buf
}

// BuildEOFPacket builds a MySQL EOF packet (header 0xFE).
func BuildEOFPacket(warnings uint16, statusFlags uint16) []byte {
	var buf []byte
	buf = append(buf, EOFPacket)

	warn := make([]byte, 2)
	binary.LittleEndian.PutUint16(warn, warnings)
	buf = append(buf, warn...)

	status := make([]byte, 2)
	binary.LittleEndian.PutUint16(status, statusFlags)
	buf = append(buf, status...)

	return buf
}

// ColumnDef41 represents a column definition in a result set (ProtocolText::Resultset).
type ColumnDef41 struct {
	Catalog      string
	Schema       string
	Table        string
	OrgTable     string
	Name         string
	OrgName      string
	CharacterSet uint16
	ColumnLength uint32
	ColumnType   byte
	Flags        uint16
	Decimals     byte
}

// Encode encodes ColumnDef41 into wire format bytes.
func (c *ColumnDef41) Encode() []byte {
	var buf []byte

	// Length-encoded strings for identifiers
	buf = append(buf, PutLenEncString(c.Catalog)...)
	buf = append(buf, PutLenEncString(c.Schema)...)
	buf = append(buf, PutLenEncString(c.Table)...)
	buf = append(buf, PutLenEncString(c.OrgTable)...)
	buf = append(buf, PutLenEncString(c.Name)...)
	buf = append(buf, PutLenEncString(c.OrgName)...)

	// Length of fixed fields (always 0x0C = 12)
	buf = append(buf, PutLenEncInt(0x0C)...)

	// Character set (2 bytes)
	cs := make([]byte, 2)
	binary.LittleEndian.PutUint16(cs, c.CharacterSet)
	buf = append(buf, cs...)

	// Column length (4 bytes)
	cl := make([]byte, 4)
	binary.LittleEndian.PutUint32(cl, c.ColumnLength)
	buf = append(buf, cl...)

	// Column type (1 byte)
	buf = append(buf, c.ColumnType)

	// Flags (2 bytes)
	fl := make([]byte, 2)
	binary.LittleEndian.PutUint16(fl, c.Flags)
	buf = append(buf, fl...)

	// Decimals (1 byte)
	buf = append(buf, c.Decimals)

	// Filler (2 bytes)
	buf = append(buf, 0x00, 0x00)

	return buf
}

// BuildColumnDef creates a ColumnDef41 from a column name.
func BuildColumnDef(name string) *ColumnDef41 {
	return &ColumnDef41{
		Catalog:      "def",
		Name:         name,
		OrgName:      name,
		CharacterSet: uint16(CharsetUTF8MB4),
		ColumnLength: 1024,
		ColumnType:   TypeVarString,
	}
}

// PutLenEncInt encodes an unsigned integer in MySQL length-encoded format.
func PutLenEncInt(v uint64) []byte {
	switch {
	case v < 251:
		return []byte{byte(v)}
	case v < 1<<16:
		buf := make([]byte, 3)
		buf[0] = 0xFC
		binary.LittleEndian.PutUint16(buf[1:], uint16(v))
		return buf
	case v < 1<<24:
		buf := make([]byte, 4)
		buf[0] = 0xFD
		buf[1] = byte(v)
		buf[2] = byte(v >> 8)
		buf[3] = byte(v >> 16)
		return buf
	default:
		buf := make([]byte, 9)
		buf[0] = 0xFE
		binary.LittleEndian.PutUint64(buf[1:], v)
		return buf
	}
}

// PutLenEncString encodes a string in MySQL length-encoded format.
func PutLenEncString(s string) []byte {
	b := []byte(s)
	return append(PutLenEncInt(uint64(len(b))), b...)
}

// BuildResultSetRow builds a text protocol result set row from string values.
// NULL values are encoded as 0xFB.
func BuildResultSetRow(values []string) []byte {
	var buf []byte
	for _, v := range values {
		if v == "NULL" {
			buf = append(buf, 0xFB)
		} else {
			buf = append(buf, PutLenEncString(v)...)
		}
	}
	return buf
}

// DecodeCommand extracts the command byte from a MySQL packet payload.
// Returns the command byte and the remaining payload.
func DecodeCommand(payload []byte) (cmd byte, data []byte, err error) {
	if len(payload) < 1 {
		return 0, nil, fmt.Errorf("empty command payload")
	}
	return payload[0], payload[1:], nil
}
