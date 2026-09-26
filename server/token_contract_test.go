package main

import (
	"encoding/json/v2"
	"math/big"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCompactValue(t *testing.T) {
	long := "a" + strings.Repeat("я", maxCellBytes) // "я" is two bytes, so the cut lands mid-rune
	tests := []struct {
		name string
		in   any
		want any
	}{
		{"nil", nil, nil},
		{"short string", "abc", "abc"},
		{"uuid", [16]byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0}, "12345678-9abc-def0-1234-56789abcdef0"},
		{"bytea", []byte{0xde, 0xad, 0xbe, 0xef}, `\xdeadbeef`},
		{"interval", pgtype.Interval{Microseconds: 7200000000, Days: 1, Valid: true}, "1 day 02:00:00"},
		{"numeric stays a number", pgtype.Numeric{Int: big.NewInt(15), Exp: -1, Valid: true}, 1.5},
		{"number", int64(42), 42},
		{"uuid array", []any{[16]byte{}, nil}, []any{"00000000-0000-0000-0000-000000000000", nil}},
		{"long string", long, long[:maxCellBytes-1] + "…(1001 bytes)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotJSON, err := json.Marshal(compactValue(tt.in))
			if err != nil {
				t.Fatalf("json.Marshal(compactValue(%v)) error = %v", tt.in, err)
			}
			wantJSON, _ := json.Marshal(tt.want)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("compactValue(%v) = %s, want %s", tt.in, gotJSON, wantJSON)
			}
		})
	}
}

func TestCompactValueLongJSON(t *testing.T) {
	doc := map[string]any{"k": strings.Repeat("x", 2*maxCellBytes)}
	got, ok := compactValue(doc).(string)
	if !ok || !strings.HasPrefix(got, `{"k":"xxx`) || len(got) > maxCellBytes+len("…(9999 bytes)") {
		t.Errorf("compactValue(jsonb of %d bytes) = %q, want its JSON text cut to %d bytes", 2*maxCellBytes, got, maxCellBytes)
	}
}

func TestToolsList(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want []string
	}{
		{"read only", Config{}, []string{"describe_table", "list_schemas", "list_tables", "query", "search"}},
		{"writes and runtime connect", Config{AllowWrite: true, EnableRuntimeConnect: true}, []string{"connect_db", "describe_table", "execute", "list_schemas", "list_tables", "query", "search"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := connectInMemory(t, &Server{cfg: tt.cfg})
			res, err := session.ListTools(t.Context(), nil)
			if err != nil {
				t.Fatalf("ListTools() error = %v", err)
			}
			var names []string
			for _, tool := range res.Tools {
				names = append(names, tool.Name)
				if tool.OutputSchema != nil {
					t.Errorf("ListTools(%+v) tool %q OutputSchema = %v, want none", tt.cfg, tool.Name, tool.OutputSchema)
				}
			}
			slices.Sort(names)
			if !slices.Equal(names, tt.want) {
				t.Errorf("ListTools(%+v) = %v, want %v", tt.cfg, names, tt.want)
			}
		})
	}
}

func TestToolResults(t *testing.T) {
	db := mustPool(t)
	defer db.Close()
	resetSchema(t, db)
	srv, err := newServer(t.Context(), Config{DatabaseURL: os.Getenv("DATABASE_URL"), QueryTO: 5 * time.Second, MaxRows: 3})
	if err != nil {
		t.Fatalf("newServer() error = %v", err)
	}
	defer func() { _ = srv.Shutdown(t.Context()) }()
	session := connectInMemory(t, srv)

	tests := []struct {
		name          string
		tool          string
		args          map[string]any
		wantErr       bool
		wantColumns   []string
		wantRows      int
		wantTruncated bool
	}{
		// A billion rows answer within QueryTO only if the database stops at the limit.
		{"stops at the limit", "query", map[string]any{"sql": "SELECT generate_series(1, 1000000000) AS n, gen_random_uuid() AS u"}, false, []string{"n", "u"}, 3, true},
		{"max_rows below MAX_ROWS", "query", map[string]any{"sql": "SELECT generate_series(1, 10) AS n", "max_rows": 2}, false, []string{"n"}, 2, true},
		{"duplicate column names", "query", map[string]any{"sql": "SELECT 1 AS a, 2 AS a"}, false, []string{"a", "a"}, 1, false},
		{"trailing semicolon and comment", "query", map[string]any{"sql": "SELECT 1 AS n; -- done"}, false, []string{"n"}, 1, false},
		{"params", "query", map[string]any{"sql": "WITH x AS (SELECT $1::int AS n) SELECT n FROM x", "params": []any{7}}, false, []string{"n"}, 1, false},
		{"explain is not wrapped", "query", map[string]any{"sql": "EXPLAIN SELECT 1"}, false, []string{"QUERY PLAN"}, 1, false},
		{"two statements", "query", map[string]any{"sql": "SELECT 1; SELECT 2"}, true, nil, 0, false},
		{"write in a read-only query", "query", map[string]any{"sql": "WITH d AS (DELETE FROM users RETURNING id) SELECT * FROM d"}, true, nil, 0, false},
		{"search", "search", map[string]any{"q": "Cable", "limit": 5}, false, []string{"source_table", "column", "match_text"}, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tt.tool, Arguments: tt.args})
			if err != nil {
				t.Fatalf("CallTool(%s, %v) error = %v", tt.tool, tt.args, err)
			}
			if res.IsError != tt.wantErr {
				t.Fatalf("CallTool(%s, %v) IsError = %t, want %t: %v", tt.tool, tt.args, res.IsError, tt.wantErr, res.Content)
			}
			if tt.wantErr {
				return
			}
			if res.StructuredContent != nil || len(res.Content) != 1 {
				t.Fatalf("CallTool(%s, %v) = %d content blocks, structured %v; want one text block only", tt.tool, tt.args, len(res.Content), res.StructuredContent)
			}
			text := res.Content[0].(*mcp.TextContent).Text
			var got struct {
				Columns   []string `json:"columns"`
				Rows      [][]any  `json:"rows"`
				Truncated bool     `json:"truncated"`
				SQL       *string  `json:"sql"`
			}
			if err := json.Unmarshal([]byte(text), &got); err != nil {
				t.Fatalf("CallTool(%s, %v) text %q: %v", tt.tool, tt.args, text, err)
			}
			if !slices.Equal(got.Columns, tt.wantColumns) || len(got.Rows) < tt.wantRows || got.Truncated != tt.wantTruncated || got.SQL != nil {
				t.Errorf("CallTool(%s, %v) = %s, want columns %v, at least %d rows, truncated %t, no sql", tt.tool, tt.args, text, tt.wantColumns, tt.wantRows, tt.wantTruncated)
			}
			if tt.wantTruncated && len(got.Rows) != tt.wantRows {
				t.Errorf("CallTool(%s, %v) = %d rows, want %d", tt.tool, tt.args, len(got.Rows), tt.wantRows)
			}
		})
	}
}

// The server usually runs as a role with SELECT only, which information_schema
// hides constraints from.
func TestReaderResults(t *testing.T) {
	db := mustPool(t)
	defer db.Close()
	resetSchema(t, db)
	if _, err := db.Exec(t.Context(), `
CREATE TABLE alembic_version (version_num varchar(32) NOT NULL, note text, CONSTRAINT alembic_version_pkc PRIMARY KEY (version_num));
DROP ROLE IF EXISTS postgres_mcp_reader;
CREATE ROLE postgres_mcp_reader LOGIN PASSWORD 'reader';
GRANT USAGE ON SCHEMA public TO postgres_mcp_reader;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO postgres_mcp_reader;`); err != nil {
		t.Fatalf("create reader role: %v", err)
	}
	dsn, err := url.Parse(db.Config().ConnString())
	if err != nil {
		t.Fatalf("url.Parse(DATABASE_URL) error = %v", err)
	}
	dsn.User = url.UserPassword("postgres_mcp_reader", "reader")
	srv, err := newServer(t.Context(), Config{DatabaseURL: dsn.String(), QueryTO: 5 * time.Second, MaxRows: 3})
	if err != nil {
		t.Fatalf("newServer() error = %v", err)
	}
	defer func() { _ = srv.Shutdown(t.Context()) }()
	session := connectInMemory(t, srv)

	tests := []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{"primary key", "describe_table", map[string]any{"table": "alembic_version"},
			`{"schema":"public","table":"alembic_version","columns":[{"name":"version_num","data_type":"character varying","nullable":false,"primary_key":true},{"name":"note","data_type":"text","nullable":true,"primary_key":false}]}`},
		{"char as text", "query", map[string]any{"sql": `SELECT c.contype, r.relkind, NULL::"char" AS none, '{i,o,NULL}'::"char"[] AS modes, 112 AS n FROM pg_constraint c JOIN pg_class r ON r.oid = c.conrelid WHERE c.conname = 'alembic_version_pkc'`},
			`{"columns":["contype","relkind","none","modes","n"],"rows":[["p","r",null,["i","o",null],112]]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tt.tool, Arguments: tt.args})
			if err != nil {
				t.Fatalf("CallTool(%s, %v) error = %v", tt.tool, tt.args, err)
			}
			if got := res.Content[0].(*mcp.TextContent).Text; got != tt.want {
				t.Errorf("CallTool(%s, %v) = %s, want %s", tt.tool, tt.args, got, tt.want)
			}
		})
	}
}

func TestExecuteOneStatement(t *testing.T) {
	db := mustPool(t)
	defer db.Close()
	resetSchema(t, db)
	srv, err := newServer(t.Context(), Config{DatabaseURL: os.Getenv("DATABASE_URL"), QueryTO: 5 * time.Second, MaxRows: 3, AllowWrite: true})
	if err != nil {
		t.Fatalf("newServer() error = %v", err)
	}
	defer func() { _ = srv.Shutdown(t.Context()) }()
	session := connectInMemory(t, srv)

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
		want    string
	}{
		{"no params", map[string]any{"sql": "UPDATE users SET first_name = first_name WHERE id = 1"}, false, `{"command":"UPDATE 1","row_count":1}`},
		{"params", map[string]any{"sql": "UPDATE users SET first_name = first_name WHERE id = $1", "params": []any{1}}, false, `{"command":"UPDATE 1","row_count":1}`},
		{"two statements without params", map[string]any{"sql": "UPDATE users SET first_name = 'x' WHERE id = 1; DELETE FROM users"}, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "execute", Arguments: tt.args})
			if err != nil {
				t.Fatalf("CallTool(execute, %v) error = %v", tt.args, err)
			}
			if res.IsError != tt.wantErr {
				t.Fatalf("CallTool(execute, %v) IsError = %t, want %t: %v", tt.args, res.IsError, tt.wantErr, res.Content)
			}
			if got := res.Content[0].(*mcp.TextContent).Text; !tt.wantErr && got != tt.want {
				t.Errorf("CallTool(execute, %v) = %s, want %s", tt.args, got, tt.want)
			}
		})
	}
	var users int
	if err := db.QueryRow(t.Context(), "SELECT count(*) FROM users").Scan(&users); err != nil || users == 0 {
		t.Errorf("users after execute = %d (err %v), want the rejected DELETE to leave them", users, err)
	}
}

func connectInMemory(t *testing.T, srv *Server) *mcp.ClientSession {
	t.Helper()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	if _, err := newMCPServer(srv).Connect(t.Context(), serverTransport, nil); err != nil {
		t.Fatalf("server Connect() error = %v", err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}
