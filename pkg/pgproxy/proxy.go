package pgproxy

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgproto3"

	"github.com/http-pg/http-pg/pkg/httpclient"
	"github.com/http-pg/http-pg/pkg/pgparser"
)

// Proxy is a PgSQL TCP proxy that forwards messages to an HTTP API server.
type Proxy struct {
	listenAddr string
	httpClient *httpclient.Client
	listener   net.Listener
	wg         sync.WaitGroup
}

// New creates a new Proxy.
func New(listenAddr, serverURL, encKey string) (*Proxy, error) {
	client, err := httpclient.NewClient(serverURL, encKey)
	if err != nil {
		return nil, fmt.Errorf("create http client: %w", err)
	}

	return &Proxy{
		listenAddr: listenAddr,
		httpClient: client,
	}, nil
}

// Start begins listening for PgSQL client connections.
func (p *Proxy) Start() error {
	var err error
	p.listener, err = net.Listen("tcp", p.listenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", p.listenAddr, err)
	}

	log.Printf("[Proxy] Listening on %s", p.listenAddr)

	for {
		conn, err := p.listener.Accept()
		if err != nil {
			log.Printf("[Proxy] Accept error: %v", err)
			continue
		}

		p.wg.Add(1)
		go p.handleConnection(conn)
	}
}

// Stop closes the listener and waits for all connections to finish.
func (p *Proxy) Stop() error {
	if p.listener != nil {
		p.listener.Close()
	}
	p.wg.Wait()
	return nil
}

func (p *Proxy) handleConnection(clientConn net.Conn) {
	defer p.wg.Done()
	defer clientConn.Close()

	sessionID := uuid.New().String()
	log.Printf("[Proxy] New connection: session=%s, remote=%s", sessionID[:8], clientConn.RemoteAddr())

	// Create Backend to communicate with PgSQL client
	// Backend reads frontend messages from client, sends backend messages to client
	backend := pgproto3.NewBackend(clientConn, clientConn)

	// Read the StartupMessage
	rawStartup, err := pgparser.ReadStartupRaw(clientConn)
	if err != nil {
		log.Printf("[Proxy] Read startup message error: %v", err)
		return
	}

	// Parse startup params for logging
	startupData, _ := pgparser.ParseStartupMessage(rawStartup)
	if startupData != nil {
		log.Printf("[Proxy] Session %s: Startup params=%v", sessionID[:8], startupData.Parameters)
	}

	// Send auth ok
	backend.Send(&pgproto3.AuthenticationOk{})
	backend.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: "14.0"})
	backend.Send(&pgproto3.ParameterStatus{Name: "server_encoding", Value: "UTF8"})
	backend.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
	backend.Send(&pgproto3.ParameterStatus{Name: "DateStyle", Value: "ISO, MDY"})
	backend.Send(&pgproto3.BackendKeyData{ProcessID: 1234, SecretKey: []byte{0x01, 0x02, 0x03, 0x04}})
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if err := backend.Flush(); err != nil {
		log.Printf("[Proxy] Flush startup response error: %v", err)
		return
	}

	// Message loop: read client messages, send to server, write responses
	for {
		msg, err := backend.Receive()
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				log.Printf("[Proxy] Session %s: Client disconnected", sessionID[:8])
			} else {
				log.Printf("[Proxy] Session %s: Read message error: %v", sessionID[:8], err)
			}
			break
		}

		encoded, err := msg.Encode(nil)
		if err != nil {
			log.Printf("[Proxy] Session %s: Encode message error: %v", sessionID[:8], err)
			break
		}

		if len(encoded) == 0 {
			continue
		}

		msgType := encoded[0]
		payload := encoded[1:]
		sql := pgparser.ExtractSQL(msgType, payload)

		if pgparser.IsTerminate(msgType) {
			log.Printf("[Proxy] Session %s: Terminate received", sessionID[:8])
			break
		}

		if sql != "" {
			log.Printf("[Proxy] Session %s: SQL: %s", sessionID[:8], sql)
		} else {
			log.Printf("[Proxy] Session %s: Message type=%s", sessionID[:8], pgparser.MessageTypeName(msgType))
		}

		// Build request and send to server
		req := pgMessageRequest{
			Type: msgType,
			SQL:  sql,
			Raw:  payload,
		}
		reqJSON, err := json.Marshal(req)
		if err != nil {
			log.Printf("[Proxy] Session %s: Marshal request error: %v", sessionID[:8], err)
			break
		}

		// Send to HTTP server
		respBytes, err := p.httpClient.QueryRequest(sessionID, reqJSON)
		if err != nil {
			log.Printf("[Proxy] Session %s: HTTP query error: %v", sessionID[:8], err)
			p.sendError(backend, fmt.Sprintf("Query failed: %v", err))
			break
		}

		// Parse the response
		var queryResult pgQueryResponse
		if err := json.Unmarshal(respBytes, &queryResult); err != nil {
			log.Printf("[Proxy] Session %s: Unmarshal response error: %v", sessionID[:8], err)
			break
		}

		if queryResult.Error != "" {
			p.sendError(backend, queryResult.Error)
			continue
		}

		// Write response back to client
		if len(queryResult.Columns) > 0 {
			rd := &pgproto3.RowDescription{}
			for _, col := range queryResult.Columns {
				rd.Fields = append(rd.Fields, pgproto3.FieldDescription{
					Name:                 []byte(col),
					TableOID:             0,
					TableAttributeNumber: 0,
					DataTypeOID:          25,
					DataTypeSize:         -1,
					TypeModifier:         -1,
					Format:               0,
				})
			}
			backend.Send(rd)
		}

		for _, row := range queryResult.Rows {
			dr := &pgproto3.DataRow{}
			for _, val := range row {
				if val == "NULL" {
					dr.Values = append(dr.Values, nil)
				} else {
					dr.Values = append(dr.Values, []byte(val))
				}
			}
			backend.Send(dr)
		}

		tag := []byte("SELECT")
		if queryResult.RowsAffected > 0 {
			tag = []byte(fmt.Sprintf("SELECT %d", queryResult.RowsAffected))
		} else {
			tag = []byte(fmt.Sprintf("SELECT %d", len(queryResult.Rows)))
		}
		backend.Send(&pgproto3.CommandComplete{CommandTag: tag})
		backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})

		if err := backend.Flush(); err != nil {
			log.Printf("[Proxy] Session %s: Flush response error: %v", sessionID[:8], err)
			break
		}
	}

	// Cleanup session
	p.httpClient.CloseSession(sessionID)
	log.Printf("[Proxy] Session %s: Connection closed", sessionID[:8])
}

func (p *Proxy) sendError(backend *pgproto3.Backend, errMsg string) {
	backend.Send(&pgproto3.ErrorResponse{
		Severity: "ERROR",
		Message:  errMsg,
		Code:     "08000",
	})
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	backend.Flush()
}

type pgMessageRequest struct {
	Type byte   `json:"type"`
	SQL  string `json:"sql"`
	Raw  []byte `json:"raw,omitempty"`
}

type pgQueryResponse struct {
	Columns      []string   `json:"columns"`
	Rows         [][]string `json:"rows"`
	RowsAffected int64      `json:"rows_affected"`
	Error        string     `json:"error,omitempty"`
}
