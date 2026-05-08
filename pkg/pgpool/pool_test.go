package pgpool

import (
	"testing"
)

func TestQueryResult(t *testing.T) {
	// Test QueryResult struct without actual DB connection
	qr := QueryResult{
		Columns:      []string{"id", "name"},
		Rows:         [][]string{{"1", "Alice"}, {"2", "Bob"}},
		RowsAffected: 2,
	}

	if len(qr.Columns) != 2 {
		t.Errorf("expected 2 columns, got %d", len(qr.Columns))
	}
	if qr.Columns[0] != "id" {
		t.Errorf("expected 'id', got %q", qr.Columns[0])
	}
	if len(qr.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(qr.Rows))
	}
	if qr.Rows[1][1] != "Bob" {
		t.Errorf("expected 'Bob', got %q", qr.Rows[1][1])
	}
}

func TestQueryResultEmpty(t *testing.T) {
	qr := QueryResult{}
	if len(qr.Columns) != 0 {
		t.Errorf("expected 0 columns, got %d", len(qr.Columns))
	}
	if len(qr.Rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(qr.Rows))
	}
	if qr.RowsAffected != 0 {
		t.Errorf("expected 0 rows affected, got %d", qr.RowsAffected)
	}
}
