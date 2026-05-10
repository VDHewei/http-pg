package mysqlproxy

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/VDHewei/http-pg/pkg/httpclient"
)

// Proxy is a MySQL TCP proxy that forwards messages to an HTTP API server.
type Proxy struct {
	listenAddr string
	httpClient *httpclient.Client
	listener   net.Listener
	wg         sync.WaitGroup
	quit       chan struct{}
}

// New creates a new MySQL Proxy.
func New(listenAddr, serverURL, encKey string) (*Proxy, error) {
	client, err := httpclient.NewClient(serverURL, encKey)
	if err != nil {
		return nil, fmt.Errorf("create http client: %w", err)
	}

	return &Proxy{
		listenAddr: listenAddr,
		httpClient: client,
		quit:       make(chan struct{}),
	}, nil
}

// Start begins listening for MySQL client connections.
func (p *Proxy) Start() error {
	var err error
	p.listener, err = net.Listen("tcp", p.listenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", p.listenAddr, err)
	}

	log.Printf("[MySQLProxy] Listening on %s", p.listenAddr)

	for {
		conn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-p.quit:
				log.Printf("[MySQLProxy] Shutting down listener")
				return nil
			default:
			}
			log.Printf("[MySQLProxy] Accept error: %v", err)
			continue
		}

		p.wg.Add(1)
		go p.handleConnection(conn)
	}
}

// Stop closes the listener and waits for all connections to finish.
func (p *Proxy) Stop() error {
	if p.listener != nil {
		close(p.quit)
		p.listener.Close()
	}
	p.wg.Wait()
	return nil
}

// handleConnection handles a single MySQL client connection.
func (p *Proxy) handleConnection(clientConn net.Conn) {
	defer p.wg.Done()
	defer clientConn.Close()

	var sessionID string
	var seq byte

	// Step 1: Send HandshakeV10
	handshake := BuildHandshakeV10()
	if err := WritePacket(clientConn, handshake.Encode(), seq); err != nil {
		log.Printf("[MySQLProxy] Send handshake error: %v", err)
		return
	}
	seq++

	// Step 2: Read HandshakeResponse41
	payload, s, err := ReadPacket(clientConn)
	if err != nil {
		log.Printf("[MySQLProxy] Read handshake response error: %v", err)
		return
	}
	seq = s + 1

	resp, err := ParseHandshakeResponse(payload)
	if err != nil {
		log.Printf("[MySQLProxy] Parse handshake response error: %v", err)
		_ = WritePacket(clientConn, BuildERRPacket(1045, "28000", "Access denied"), seq)
		return
	}
	log.Printf("[MySQLProxy] Connection from user=%q database=%q", resp.Username, resp.Database)

	// Step 3: Create session on HTTP server
	serverSessionID, err := p.httpClient.SessionRequest(payload, "mysql")
	if err != nil {
		log.Printf("[MySQLProxy] Create session error: %v", err)
		_ = WritePacket(clientConn, BuildERRPacket(1040, "08004", "Too many connections"), seq)
		return
	}
	sessionID = serverSessionID
	log.Printf("[MySQLProxy] Session %s: Created on server", sessionID[:8])

	// Step 4: Send auth OK
	if err := WritePacket(clientConn, BuildOKPacket(0, 0, ServerStatusAutocommit, 0, ""), seq); err != nil {
		log.Printf("[MySQLProxy] Send OK error: %v", err)
		return
	}
	seq++

	// Step 5: Command loop
	for {
		payload, s, err := ReadPacket(clientConn)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				log.Printf("[MySQLProxy] Session %s: Client disconnected", sessionID[:8])
			} else {
				log.Printf("[MySQLProxy] Session %s: Read packet error: %v", sessionID[:8], err)
			}
			break
		}
		seq = s + 1

		if len(payload) == 0 {
			continue
		}

		cmd := payload[0]
		// data starts after command byte
		data := payload[1:]

		switch cmd {
		case ComQuery:
			sql := string(data)
			log.Printf("[MySQLProxy] Session %s: SQL: %s", sessionID[:8], sql)

			// Build request JSON and send to HTTP server
			req := queryRequest{
				Type: cmd,
				SQL:  sql,
			}
			reqJSON, err := json.Marshal(req)
			if err != nil {
				log.Printf("[MySQLProxy] Session %s: Marshal request error: %v", sessionID[:8], err)
				break
			}

			respBytes, err := p.httpClient.QueryRequest(sessionID, reqJSON)
			if err != nil {
				log.Printf("[MySQLProxy] Session %s: HTTP query error: %v", sessionID[:8], err)
				_ = WritePacket(clientConn, BuildERRPacket(2006, "HY000", "MySQL server has gone away"), seq)
				seq++
				break
			}

			var result queryResponse
			if err := json.Unmarshal(respBytes, &result); err != nil {
				log.Printf("[MySQLProxy] Session %s: Unmarshal response error: %v", sessionID[:8], err)
				break
			}

			if result.Error != "" {
				_ = WritePacket(clientConn, BuildERRPacket(1064, "42000", result.Error), seq)
				seq++
				continue
			}

			// Build and send result set
			packets := p.buildResultSet(&result, sql)
			for _, packet := range packets {
				if err := WritePacket(clientConn, packet, seq); err != nil {
					log.Printf("[MySQLProxy] Session %s: Write result error: %v", sessionID[:8], err)
					break
				}
				seq++
			}

		case ComPing:
			if err := WritePacket(clientConn, BuildOKPacket(0, 0, ServerStatusAutocommit, 0, ""), seq); err != nil {
				log.Printf("[MySQLProxy] Session %s: Write ping response error: %v", sessionID[:8], err)
				break
			}
			seq++

		case ComQuit:
			log.Printf("[MySQLProxy] Session %s: Quit received", sessionID[:8])
			goto cleanup

		case ComInitDB:
			// Simple InitDB: return OK (actual DB switching is not implemented)
			dbName := string(data)
			log.Printf("[MySQLProxy] Session %s: InitDB to %q", sessionID[:8], dbName)
			if err := WritePacket(clientConn, BuildOKPacket(0, 0, ServerStatusAutocommit, 0, ""), seq); err != nil {
				log.Printf("[MySQLProxy] Session %s: Write InitDB response error: %v", sessionID[:8], err)
				break
			}
			seq++

		case ComStmtPrepare, ComStmtExecute, ComStmtClose, ComStmtReset, ComStmtFetch:
			_ = WritePacket(clientConn, BuildERRPacket(1295, "HY000",
				"This command is not supported in the prepared statement protocol"), seq)
			seq++

		default:
			log.Printf("[MySQLProxy] Session %s: Unknown command 0x%02x", sessionID[:8], cmd)
			_ = WritePacket(clientConn, BuildERRPacket(1047, "08S01",
				fmt.Sprintf("Unknown command: 0x%02x", cmd)), seq)
			seq++
		}
	}

cleanup:
	// Cleanup session on HTTP server
	p.httpClient.CloseSession(sessionID)
	log.Printf("[MySQLProxy] Session %s: Connection closed", sessionID[:8])
}

// buildResultSet converts a QueryResponse into MySQL result set packets.
// Uses text protocol: column_count → column_def* → EOF → row* → OK/EOF
func (p *Proxy) buildResultSet(result *queryResponse, sql string) [][]byte {
	var packets [][]byte

	// Only return column count + EOF if there are no columns (DML statements)
	if len(result.Columns) == 0 {
		// For DML statements, OK packet with affected rows
		info := commandTag(sql, result.RowsAffected)
		packets = append(packets, BuildOKPacket(uint64(result.RowsAffected), 0,
			ServerStatusAutocommit, 0, info))
		return packets
	}

	// Column count packet
	packets = append(packets, PutLenEncInt(uint64(len(result.Columns))))

	// Column definitions
	for _, col := range result.Columns {
		cd := BuildColumnDef(col)
		packets = append(packets, cd.Encode())
	}

	// EOF after columns (unless CLIENT_DEPRECATE_EOF)
	packets = append(packets, BuildEOFPacket(0, ServerStatusAutocommit))

	// Data rows
	for _, row := range result.Rows {
		packets = append(packets, BuildResultSetRow(row))
	}

	// EOF after rows
	packets = append(packets, BuildEOFPacket(0, ServerStatusAutocommit))

	return packets
}

// commandTag builds a human-readable status string for OK packet info field.
// Examples: "Records: 3  Duplicates: 0  Warnings: 0", "Affected rows: 1"
func commandTag(sql string, rowsAffected int64) string {
	cmd := sqlCommandType(sql)
	switch cmd {
	case "INSERT":
		return fmt.Sprintf("Records: %d  Duplicates: 0  Warnings: 0", rowsAffected)
	case "UPDATE", "DELETE":
		return fmt.Sprintf("Affected rows: %d  Rows matched: %d  Changed: %d  Warnings: 0",
			rowsAffected, rowsAffected, rowsAffected)
	default:
		if rowsAffected > 0 {
			return fmt.Sprintf("Affected rows: %d", rowsAffected)
		}
		return ""
	}
}

// sqlCommandType extracts the SQL command type from a query string.
func sqlCommandType(sql string) string {
	i := 0
	// Skip leading whitespace
	for i < len(sql) && (sql[i] == ' ' || sql[i] == '\t' || sql[i] == '\n' || sql[i] == '\r') {
		i++
	}
	j := i
	// Read first word
	for j < len(sql) && sql[j] != ' ' && sql[j] != '\t' && sql[j] != '\n' &&
		sql[j] != '\r' && sql[j] != '(' && sql[j] != ';' {
		j++
	}
	if j <= i {
		return ""
	}
	return sql[i:j]
}

// queryRequest is sent to the HTTP server for each SQL command.
type queryRequest struct {
	Type byte   `json:"type"`
	SQL  string `json:"sql"`
}

// queryResponse is received from the HTTP server.
type queryResponse struct {
	Columns      []string   `json:"columns"`
	Rows         [][]string `json:"rows"`
	RowsAffected int64      `json:"rows_affected"`
	Error        string     `json:"error,omitempty"`
}
