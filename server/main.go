// server/main.go
package main

import (
	"context"
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Build-time variables (set via ldflags)
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const (
	defaultQueryTimeout = 25 * time.Second
	defaultMaxRows      = 50
	maxRequestSize      = 1024 * 1024 // 1MB max request size
	maxQueryLength      = 10000       // Max query length in characters
	maxCellBytes        = 500         // Longer values are cut so one cell cannot flood the model's context
)

type Server struct {
	dbMu   sync.RWMutex
	db     *pgxpool.Pool
	cfg    Config
	server *http.Server
}

type Config struct {
	DatabaseURL          string
	QueryTO              time.Duration
	MaxRows              int
	AllowWrite           bool
	EnableRuntimeConnect bool
}

// Validate checks if the configuration is valid and returns detailed errors
func (c *Config) Validate() error {
	var errs []string

	if c.DatabaseURL == "" {
		errs = append(errs, "DATABASE_URL is required (or POSTGRES_HOST/POSTGRES_PORT/POSTGRES_DATABASE/POSTGRES_USER/POSTGRES_PASSWORD)")
	}

	if c.MaxRows <= 0 {
		errs = append(errs, "MAX_ROWS must be greater than 0")
	} else if c.MaxRows > 10000 {
		errs = append(errs, "MAX_ROWS cannot exceed 10000 (too many rows could cause memory issues)")
	}

	if c.QueryTO < time.Second {
		errs = append(errs, "QUERY_TIMEOUT must be at least 1 second")
	} else if c.QueryTO > 5*time.Minute {
		errs = append(errs, "QUERY_TIMEOUT cannot exceed 5 minutes")
	}

	if len(errs) > 0 {
		return fmt.Errorf("configuration validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

func mustConfig() Config {
	var warnings []string

	qto := defaultQueryTimeout
	if v := os.Getenv("QUERY_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			qto = d
		} else {
			warnings = append(warnings, fmt.Sprintf("invalid QUERY_TIMEOUT '%s': %v, using default %v", v, err, defaultQueryTimeout))
		}
	}

	mr := defaultMaxRows
	if v := os.Getenv("MAX_ROWS"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &mr); n == 1 && err == nil && mr > 0 {
			// Successfully parsed
		} else {
			warnings = append(warnings, fmt.Sprintf("invalid MAX_ROWS '%s': must be a positive integer, using default %d", v, defaultMaxRows))
			mr = defaultMaxRows
		}
	}

	databaseURL := resolveDatabaseURL()

	cfg := Config{
		DatabaseURL:          databaseURL,
		QueryTO:              qto,
		MaxRows:              mr,
		AllowWrite:           strings.EqualFold(strings.TrimSpace(os.Getenv("PG_ALLOW_WRITE")), "true"),
		EnableRuntimeConnect: strings.EqualFold(strings.TrimSpace(os.Getenv("PG_ENABLE_RUNTIME_CONNECT")), "true"),
	}

	// Print warnings
	for _, warning := range warnings {
		log.Warn().Msg(warning)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		log.Fatal().Err(err).Msg("invalid configuration")
	}

	return cfg
}

func envDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// resolveDatabaseURL returns DATABASE_URL if set, otherwise builds it from
// separate POSTGRES_* variables (mssql-mcp style config).
// Supported variables (POSTGRES_* take precedence, then PG_* and libpq PG*):
//
//	POSTGRES_HOST / PG_HOST / PGHOST (required if DATABASE_URL is not set)
//	POSTGRES_PORT / PG_PORT / PGPORT (default "5432")
//	POSTGRES_DATABASE / POSTGRES_DB / PG_DATABASE / PGDATABASE (required)
//	POSTGRES_USER / POSTGRES_USERNAME / PG_USER / PGUSER (required)
//	POSTGRES_PASSWORD / PG_PASSWORD / PGPASSWORD (may be empty for trust auth)
//	POSTGRES_SSLMODE / PG_SSLMODE / PGSSLMODE (default "disable")
func resolveDatabaseURL() string {
	if v := strings.TrimSpace(os.Getenv("DATABASE_URL")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("POSTGRES_URL")); v != "" {
		return v
	}

	host := firstNonEmpty(os.Getenv("POSTGRES_HOST"), os.Getenv("PG_HOST"), os.Getenv("PGHOST"))
	port := firstNonEmpty(os.Getenv("POSTGRES_PORT"), os.Getenv("PG_PORT"), os.Getenv("PGPORT"), "5432")
	database := firstNonEmpty(os.Getenv("POSTGRES_DATABASE"), os.Getenv("POSTGRES_DB"), os.Getenv("PG_DATABASE"), os.Getenv("PGDATABASE"))
	user := firstNonEmpty(os.Getenv("POSTGRES_USER"), os.Getenv("POSTGRES_USERNAME"), os.Getenv("PG_USER"), os.Getenv("PGUSER"))
	password := firstNonEmpty(os.Getenv("POSTGRES_PASSWORD"), os.Getenv("PG_PASSWORD"), os.Getenv("PGPASSWORD"))
	sslmode := firstNonEmpty(os.Getenv("POSTGRES_SSLMODE"), os.Getenv("PG_SSLMODE"), os.Getenv("PGSSLMODE"), "disable")

	if host == "" && database == "" && user == "" {
		log.Fatal().Msg("missing required env DATABASE_URL (or POSTGRES_HOST/POSTGRES_PORT/POSTGRES_DATABASE/POSTGRES_USER/POSTGRES_PASSWORD)")
	}
	if host == "" || database == "" || user == "" {
		log.Fatal().Msgf("incomplete postgres config: POSTGRES_HOST=%q POSTGRES_DATABASE=%q POSTGRES_USER=%q (password may be empty); or set DATABASE_URL", host, database, user)
	}

	u := &url.URL{
		Scheme: "postgres",
		Host:   host + ":" + port,
		Path:   "/" + database,
	}
	if password != "" {
		u.User = url.UserPassword(user, password)
	} else {
		u.User = url.User(user)
	}
	q := u.Query()
	q.Set("sslmode", sslmode)
	u.RawQuery = q.Encode()
	return u.String()
}

func newServer(ctx context.Context, cfg Config) (*Server, error) {
	db, err := newPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	return &Server{db: db, cfg: cfg}, nil
}

func newPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	conf, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	conf.MinConns = 2
	conf.MaxConns = 8
	conf.MaxConnLifetime = 30 * time.Minute
	conf.MaxConnIdleTime = 5 * time.Minute
	conf.HealthCheckPeriod = 30 * time.Second
	conf.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	db, err := pgxpool.NewWithConfig(ctx, conf)
	if err != nil {
		return nil, err
	}

	// Test the connection to ensure it's valid
	if err := db.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	log.Info().Msg("shutting down server gracefully")

	var errs []error

	// Shutdown HTTP server
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("HTTP server shutdown error: %w", err))
		}
	}

	// Close database connections
	s.dbMu.Lock()
	db := s.db
	s.db = nil
	s.dbMu.Unlock()
	if db != nil {
		db.Close()
		log.Info().Msg("database connections closed")
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}

	log.Info().Msg("server shutdown complete")
	return nil
}

// ---------- helpers ----------

func minNonZero(v, max int) int {
	if v <= 0 {
		return max
	}
	if v > max {
		return max
	}
	return v
}

// sanitizeInput sanitizes and validates user input
func sanitizeInput(input string) error {
	input = strings.TrimSpace(input)

	if len(input) == 0 {
		return errors.New("input cannot be empty")
	}

	if len(input) > maxQueryLength {
		return fmt.Errorf("input too long: %d characters (max %d)", len(input), maxQueryLength)
	}

	// Check for potentially malicious patterns
	suspicious := []string{
		"--", "/*", "*/", "xp_", "sp_", "exec", "execute",
		"union", "information_schema", "pg_catalog",
	}

	lowerInput := strings.ToLower(input)
	for _, pattern := range suspicious {
		if strings.Contains(lowerInput, pattern) {
			log.Warn().Str("pattern", pattern).Str("input", input).Msg("suspicious pattern detected in input")
		}
	}

	return nil
}

// auditLog logs security-relevant events
func auditLog(event, user, query, result string, success bool) {
	log.Info().
		Str("event", event).
		Str("user", user).
		Str("query", query).
		Str("result", result).
		Bool("success", success).
		Msg("audit_log")
}

// requestSizeLimitMiddleware limits the size of incoming requests
func requestSizeLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
		next.ServeHTTP(w, r)
	})
}

// ---------- MCP tool handlers ----------

type queryInput struct {
	SQL     string `json:"sql"`
	Params  []any  `json:"params,omitempty"`
	MaxRows int    `json:"max_rows,omitempty"`
}

// rowsOutput is columnar: column names travel once instead of once per row.
type rowsOutput struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	Truncated bool     `json:"truncated,omitzero"`
}

func (s *Server) handleQuery(ctx context.Context, _ *mcp.CallToolRequest, in queryInput) (*mcp.CallToolResult, rowsOutput, error) {
	sql := strings.TrimSpace(in.SQL)
	if err := validateSQLInput(sql); err != nil {
		return nil, rowsOutput{}, err
	}
	limit := s.cfg.MaxRows
	if in.MaxRows > 0 && in.MaxRows < limit {
		limit = in.MaxRows
	}
	switch strings.ToUpper(strings.Fields(sql)[0]) {
	case "SELECT", "WITH", "EXPLAIN", "SHOW":
	default:
		return nil, rowsOutput{}, errors.New("query must start with SELECT, WITH, EXPLAIN, or SHOW")
	}
	out, err := s.runReadOnlyQuery(ctx, sql, in.Params, limit)
	return nil, out, err
}

type executeInput struct {
	SQL    string `json:"sql"`
	Params []any  `json:"params,omitempty"`
}

type executeOutput struct {
	Command  string `json:"command"`
	RowCount int64  `json:"row_count"`
}

func (s *Server) handleExecute(ctx context.Context, _ *mcp.CallToolRequest, in executeInput) (*mcp.CallToolResult, executeOutput, error) {
	sql := strings.TrimSpace(in.SQL)
	if err := validateSQLInput(sql); err != nil {
		return nil, executeOutput{}, err
	}
	if !s.cfg.AllowWrite {
		return nil, executeOutput{}, errors.New("writes are disabled; set PG_ALLOW_WRITE=true to enable execute")
	}

	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	ctxTO, cancel := context.WithTimeout(ctx, s.cfg.QueryTO)
	defer cancel()
	// Extended protocol binds values and keeps writes to one statement; the exec
	// mode does it in one round trip without caching one-off statements. Query,
	// not Exec: pgx sends an Exec without arguments over the simple protocol,
	// which runs every statement in the string.
	execArgs := append([]any{pgx.QueryExecModeExec}, in.Params...)
	rows, err := s.db.Query(ctxTO, sql, execArgs...)
	if err != nil {
		return nil, executeOutput{}, err
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, executeOutput{}, err
	}
	tag := rows.CommandTag()
	return nil, executeOutput{Command: tag.String(), RowCount: tag.RowsAffected()}, nil
}

func validateSQLInput(sql string) error {
	if sql == "" {
		return errors.New("sql cannot be empty")
	}
	if len(sql) > maxQueryLength {
		return fmt.Errorf("sql too long: %d characters (max %d)", len(sql), maxQueryLength)
	}
	return nil
}

type listSchemasOutput struct {
	Schemas []string `json:"schemas"`
}

func (s *Server) handleListSchemas(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, listSchemasOutput, error) {
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	ctxTO, cancel := context.WithTimeout(ctx, s.cfg.QueryTO)
	defer cancel()
	rows, err := s.db.Query(ctxTO, `
SELECT schema_name
FROM information_schema.schemata
WHERE schema_name <> 'information_schema' AND left(schema_name, 3) <> 'pg_'
ORDER BY schema_name`)
	if err != nil {
		return nil, listSchemasOutput{}, err
	}
	defer rows.Close()

	schemas := make([]string, 0)
	for rows.Next() {
		var schema string
		if err := rows.Scan(&schema); err != nil {
			return nil, listSchemasOutput{}, err
		}
		schemas = append(schemas, schema)
	}
	if err := rows.Err(); err != nil {
		return nil, listSchemasOutput{}, err
	}
	return nil, listSchemasOutput{Schemas: schemas}, nil
}

type listTablesInput struct {
	Schema string `json:"schema,omitempty"`
}

type listTablesOutput struct {
	Tables []string `json:"tables"`
}

func (s *Server) handleListTables(ctx context.Context, _ *mcp.CallToolRequest, in listTablesInput) (*mcp.CallToolResult, listTablesOutput, error) {
	schema := strings.TrimSpace(in.Schema)
	if schema == "" {
		schema = "public"
	}
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	ctxTO, cancel := context.WithTimeout(ctx, s.cfg.QueryTO)
	defer cancel()
	rows, err := s.db.Query(ctxTO, `
SELECT table_name
FROM information_schema.tables
WHERE table_schema = $1 AND table_type IN ('BASE TABLE', 'VIEW')
ORDER BY table_name`, schema)
	if err != nil {
		return nil, listTablesOutput{}, err
	}
	defer rows.Close()

	tables := make([]string, 0)
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, listTablesOutput{}, err
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return nil, listTablesOutput{}, err
	}
	return nil, listTablesOutput{Tables: tables}, nil
}

type describeTableInput struct {
	Table  string `json:"table"`
	Schema string `json:"schema,omitempty"`
}

type tableColumn struct {
	Name       string  `json:"name"`
	DataType   string  `json:"data_type"`
	Nullable   bool    `json:"nullable"`
	Default    *string `json:"default,omitempty"`
	PrimaryKey bool    `json:"primary_key"`
}

type describeTableOutput struct {
	Schema  string        `json:"schema"`
	Table   string        `json:"table"`
	Columns []tableColumn `json:"columns"`
}

func (s *Server) handleDescribeTable(ctx context.Context, _ *mcp.CallToolRequest, in describeTableInput) (*mcp.CallToolResult, describeTableOutput, error) {
	table := strings.TrimSpace(in.Table)
	if table == "" {
		return nil, describeTableOutput{}, errors.New("table cannot be empty")
	}
	schema := strings.TrimSpace(in.Schema)
	if schema == "" {
		schema = "public"
	}
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	ctxTO, cancel := context.WithTimeout(ctx, s.cfg.QueryTO)
	defer cancel()
	rows, err := s.db.Query(ctxTO, `
SELECT c.column_name, c.data_type, c.is_nullable = 'YES', c.column_default,
       -- information_schema.table_constraints hides tables the role has only
       -- SELECT on; pg_constraint does not. ordinal_position is the attnum.
       EXISTS (
           SELECT 1
           FROM pg_constraint k
           WHERE k.contype = 'p'
             AND k.conrelid = format('%I.%I', c.table_schema, c.table_name)::regclass
             AND c.ordinal_position = ANY (k.conkey)
       )
FROM information_schema.columns c
WHERE c.table_schema = $1 AND c.table_name = $2
ORDER BY c.ordinal_position`, schema, table)
	if err != nil {
		return nil, describeTableOutput{}, err
	}
	defer rows.Close()

	columns := make([]tableColumn, 0)
	for rows.Next() {
		var column tableColumn
		var defaultValue *string
		if err := rows.Scan(&column.Name, &column.DataType, &column.Nullable, &defaultValue, &column.PrimaryKey); err != nil {
			return nil, describeTableOutput{}, err
		}
		column.Default = defaultValue
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, describeTableOutput{}, err
	}
	if len(columns) == 0 {
		return nil, describeTableOutput{}, fmt.Errorf("table %q not found in schema %q", table, schema)
	}
	return nil, describeTableOutput{Schema: schema, Table: table, Columns: columns}, nil
}

type connectDBInput struct {
	Host     string `json:"host"`
	Port     int    `json:"port,omitempty"`
	User     string `json:"user"`
	Password string `json:"password,omitempty"`
	Database string `json:"database"`
	SSLMode  string `json:"sslmode,omitempty"`
}

type connectDBOutput struct {
	Connected bool   `json:"connected"`
	Host      string `json:"host"`
	Database  string `json:"database"`
}

func (s *Server) handleConnectDB(ctx context.Context, _ *mcp.CallToolRequest, in connectDBInput) (*mcp.CallToolResult, connectDBOutput, error) {
	if !s.cfg.EnableRuntimeConnect {
		return nil, connectDBOutput{}, errors.New("runtime connection switching is disabled; set PG_ENABLE_RUNTIME_CONNECT=true to enable connect_db")
	}
	in.Host = strings.TrimSpace(in.Host)
	in.User = strings.TrimSpace(in.User)
	in.Database = strings.TrimSpace(in.Database)
	if in.Host == "" || in.User == "" || in.Database == "" {
		return nil, connectDBOutput{}, errors.New("host, user, and database are required")
	}
	if in.Port == 0 {
		in.Port = 5432
	}
	if in.Port < 1 || in.Port > 65535 {
		return nil, connectDBOutput{}, errors.New("port must be between 1 and 65535")
	}
	sslmode := strings.TrimSpace(in.SSLMode)
	if sslmode == "" {
		sslmode = "disable"
	}
	connURL := &url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(in.Host, strconv.Itoa(in.Port)),
		Path:   "/" + in.Database,
		User:   url.UserPassword(in.User, in.Password),
	}
	params := connURL.Query()
	params.Set("sslmode", sslmode)
	connURL.RawQuery = params.Encode()

	ctxTO, cancel := context.WithTimeout(ctx, s.cfg.QueryTO)
	defer cancel()
	db, err := newPool(ctxTO, connURL.String())
	if err != nil {
		return nil, connectDBOutput{}, fmt.Errorf("connect to database: %w", err)
	}

	s.dbMu.Lock()
	oldDB := s.db
	s.db = db
	s.dbMu.Unlock()
	if oldDB != nil {
		oldDB.Close()
	}
	auditLog("runtime_database_connected", "mcp", "", in.Host+"/"+in.Database, true)
	return nil, connectDBOutput{Connected: true, Host: in.Host, Database: in.Database}, nil
}

type searchInput struct {
	Q     string `json:"q"`
	Limit int    `json:"limit,omitempty"`
}

func (s *Server) handleSearch(ctx context.Context, req *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, rowsOutput, error) {
	start := time.Now()
	clientIP := "unknown" // MCP doesn't expose client IP directly

	log.Debug().Str("tool", "search").Str("q", strings.TrimSpace(in.Q)).Int("limit", in.Limit).Str("client_ip", clientIP).Msg("request")

	// Input sanitization and validation
	if err := sanitizeInput(in.Q); err != nil {
		auditLog("search_input_validation_failed", clientIP, in.Q, err.Error(), false)
		log.Debug().Str("tool", "search").Err(err).Msg("input validation failed")
		return nil, rowsOutput{}, err
	}
	limit := minNonZero(in.Limit, 50)
	sql, args, err := s.buildSearchSQL(ctx, in.Q, limit)
	if err != nil {
		auditLog("search_sql_build_failed", clientIP, in.Q, err.Error(), false)
		log.Debug().Str("tool", "search").Err(err).Msg("build sql failed")
		return nil, rowsOutput{}, err
	}
	log.Debug().Str("tool", "search").Str("sql", sql).Msg("generated sql")

	// The generated SQL stays in the logs: it runs to thousands of tokens and the model cannot use it.
	out, err := s.runReadOnlyQuery(ctx, sql, args, limit)
	if err != nil {
		auditLog("search_query_failed", clientIP, sql, err.Error(), false)
		log.Debug().Str("tool", "search").Err(err).Dur("dur", time.Since(start)).Msg("query failed")
		return nil, rowsOutput{}, err
	}
	auditLog("search_success", clientIP, in.Q, fmt.Sprintf("returned %d rows", len(out.Rows)), true)
	log.Debug().Str("tool", "search").Int("row_count", len(out.Rows)).Dur("dur", time.Since(start)).Msg("done")
	return nil, out, nil
}

// ---------- SQL helpers ----------

func (s *Server) runReadOnlyQuery(ctx context.Context, sql string, args []any, limit int) (rowsOutput, error) {
	if limit <= 0 {
		limit = defaultMaxRows
	}
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	ctxTO, cancel := context.WithTimeout(ctx, s.cfg.QueryTO)
	defer cancel()
	conn, err := s.db.Acquire(ctxTO)
	if err != nil {
		return rowsOutput{}, err
	}
	defer conn.Release()

	tx, err := conn.BeginTx(ctxTO, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return rowsOutput{}, err
	}
	defer func() {
		if err := tx.Rollback(ctxTO); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			log.Warn().Err(err).Msg("read-only query rollback failed")
		}
	}()

	// Extended protocol binds values and keeps queries to one statement; the exec
	// mode does it in one round trip without caching one-off statements.
	queryArgs := append([]any{pgx.QueryExecModeExec}, args...)
	if verb := strings.ToUpper(strings.Fields(sql)[0]); verb == "SELECT" || verb == "WITH" {
		// A plain query streams every row and pgx drains the rest on Close, so a
		// cursor lets the database stop at limit+1: the extra row marks truncation.
		// Unlike a LIMIT wrapper it takes the SQL as written, trailing ";" and
		// comments included. DECLARE goes through Query, not Exec: pgx sends an Exec
		// without arguments over the simple protocol, which runs several statements,
		// "COMMIT; DELETE ..." included.
		declared, err := tx.Query(ctxTO, "DECLARE q NO SCROLL CURSOR FOR "+sql, queryArgs...)
		if err != nil {
			return rowsOutput{}, err
		}
		declared.Close()
		if err := declared.Err(); err != nil {
			return rowsOutput{}, err
		}
		sql, queryArgs = "FETCH "+strconv.Itoa(limit+1)+" FROM q", []any{pgx.QueryExecModeExec}
	}
	rows, err := tx.Query(ctxTO, sql, queryArgs...)
	if err != nil {
		return rowsOutput{}, err
	}
	defer rows.Close()

	var out rowsOutput
	fields := rows.FieldDescriptions()
	for _, f := range fields {
		out.Columns = append(out.Columns, f.Name)
	}
	for rows.Next() {
		if len(out.Rows) == limit {
			out.Truncated = true
			break
		}
		vals, err := rows.Values()
		if err != nil {
			return rowsOutput{}, err
		}
		for i, v := range vals {
			// pgx decodes "char" (relkind, contype, proargmodes) as a rune, the
			// same int32 as int4, so only the column type tells it apart.
			switch fields[i].DataTypeOID {
			case pgtype.QCharOID:
				v = qcharText(v)
			case pgtype.QCharArrayOID:
				if elems, ok := v.([]any); ok {
					for j, e := range elems {
						elems[j] = qcharText(e)
					}
				}
			}
			vals[i] = compactValue(v)
		}
		out.Rows = append(out.Rows, vals)
	}
	if err := rows.Err(); err != nil {
		return rowsOutput{}, err
	}
	return out, nil
}

// compactValue rewrites values whose default JSON wastes the model's tokens or
// is unreadable: uuid as 16 numbers, bytea as base64, interval as a struct, and
// text, jsonb or arrays long enough to crowd out the rest of the result.
func compactValue(v any) any {
	switch v := v.(type) {
	case [16]byte:
		return uuid.UUID(v).String()
	case string:
		return truncateCell(v)
	case []byte:
		if len(v) <= maxCellBytes/2 {
			return fmt.Sprintf(`\x%x`, v)
		}
		return fmt.Sprintf(`\x%x…(%d bytes)`, v[:maxCellBytes/2], len(v))
	case []any:
		for i, e := range v {
			v[i] = compactValue(e)
		}
		return truncateJSON(v)
	case map[string]any:
		return truncateJSON(v)
	case json.Marshaler:
		return v
	case driver.Valuer:
		if text, err := v.Value(); err == nil {
			return text
		}
	}
	return v
}

// qcharText turns a decoded "char" into its one-character text; NULL stays nil.
func qcharText(v any) any {
	if r, ok := v.(rune); ok {
		return string(r)
	}
	return v
}

// truncateJSON keeps a jsonb or array value whole unless its JSON text is over maxCellBytes.
func truncateJSON(v any) any {
	b, err := json.Marshal(v, jsontext.AllowInvalidUTF8(true))
	if err != nil || len(b) <= maxCellBytes {
		return v
	}
	return truncateCell(string(b))
}

func truncateCell(s string) string {
	if len(s) <= maxCellBytes {
		return s
	}
	// The cut may split a rune; dropping the fragment keeps the text valid UTF-8.
	return fmt.Sprintf("%s…(%d bytes)", strings.ToValidUTF8(s[:maxCellBytes], ""), len(s))
}

func (s *Server) buildSearchSQL(ctx context.Context, q string, limit int) (string, []any, error) {
	const meta = `
SELECT table_schema, table_name, column_name
FROM information_schema.columns
WHERE data_type IN ('text','character varying','character','citext')
  AND table_schema NOT IN ('pg_catalog','information_schema')
ORDER BY table_schema, table_name, ordinal_position;
`
	ctxTO, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	rows, err := s.db.Query(ctxTO, meta)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()

	type col struct{ s, t, c string }
	var cols []col
	for rows.Next() {
		var c col
		if err := rows.Scan(&c.s, &c.t, &c.c); err != nil {
			return "", nil, err
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return "", nil, err
	}
	if len(cols) == 0 {
		return "", nil, errors.New("no searchable columns")
	}
	var parts []string
	args := []any{q}
	for _, c := range cols {
		sourceArg := len(args) + 1
		columnArg := sourceArg + 1
		args = append(args, c.s+"."+c.t, c.c)
		parts = append(parts, fmt.Sprintf(
			`SELECT CAST($%d AS text) AS source_table, CAST($%d AS text) AS "column", LEFT(CAST(%s AS text), 240) AS match_text FROM %s WHERE %s ILIKE '%%' || $1 || '%%'`,
			sourceArg, columnArg, pgx.Identifier{c.c}.Sanitize(),
			pgx.Identifier{c.s, c.t}.Sanitize(), pgx.Identifier{c.c}.Sanitize(),
		))
		if len(parts) >= 60 {
			break
		}
	}
	sql := "WITH u AS (\n" + strings.Join(parts, "\nUNION ALL\n") + fmt.Sprintf("\n) SELECT * FROM u LIMIT %d", limit)
	return sql, args, nil
}

// newMCPServer registers only the tools the configuration enables: every tool
// definition is sent to the model with each conversation.
func newMCPServer(srv *Server) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "postgres-mcp-go", Version: "0.3.0"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "query",
		Description: "Run one read-only SELECT, WITH, EXPLAIN, or SHOW statement. Use $1, $2, and params for values; results are capped by MAX_ROWS.",
	}, textResult(srv.handleQuery))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_schemas",
		Description: "List non-system schemas in the connected PostgreSQL database.",
	}, textResult(srv.handleListSchemas))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_tables",
		Description: "List tables and views in a schema, defaulting to public.",
	}, textResult(srv.handleListTables))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "describe_table",
		Description: "Describe a table's columns, types, nullability, defaults, and primary keys.",
	}, textResult(srv.handleDescribeTable))
	if srv.cfg.AllowWrite {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "execute",
			Description: "Execute one SQL statement that changes data or schema. Use $1, $2, and params for values.",
		}, textResult(srv.handleExecute))
	}
	if srv.cfg.EnableRuntimeConnect {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "connect_db",
			Description: "Switch the server to a PostgreSQL database using the supplied credentials.",
		}, textResult(srv.handleConnectDB))
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search",
		Description: "Search free text across all tables/columns (ILIKE).",
	}, textResult(srv.handleSearch))
	return server
}

// textResult sends a handler's output once, as compact JSON text. With a typed
// output the SDK would also publish an outputSchema in tools/list and send the
// same JSON a second time as structuredContent.
func textResult[In, Out any](h mcp.ToolHandlerFor[In, Out]) mcp.ToolHandlerFor[In, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		_, out, err := h(ctx, req, in)
		if err != nil {
			return nil, nil, err
		}
		text, err := json.Marshal(out, jsontext.AllowInvalidUTF8(true))
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(text)}}}, nil, nil
	}
}

func main() {
	// Handle version flag and transport selection.
	// stdio transport allows mssql-mcp style config:
	//   {"mcpServers": {"postgres": {"command": "/path/to/postgres-mcp-server", "env": {...}}}}
	versionFlag := flag.Bool("version", false, "Print version information and exit")
	transportFlag := flag.String("transport", "", "MCP transport: stdio or http (overrides MCP_TRANSPORT env, default http)")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("postgres-mcp-server %s\n", version)
		fmt.Printf("  commit: %s\n", commit)
		fmt.Printf("  built:  %s\n", date)
		os.Exit(0)
	}

	transport := strings.ToLower(strings.TrimSpace(firstNonEmpty(*transportFlag, os.Getenv("MCP_TRANSPORT"), "http")))
	if transport == "stdin" || transport == "standard" || transport == "standard-io" {
		transport = "stdio"
	}
	if transport != "stdio" && transport != "http" && transport != "streamable" && transport != "streamable-http" {
		fmt.Fprintf(os.Stderr, "unknown transport %q: must be stdio or http\n", transport)
		os.Exit(1)
	}
	isStdio := transport == "stdio"

	// In stdio mode stdout is reserved for the MCP protocol — force all logs to stderr.
	log.Logger = log.Output(os.Stderr)
	zerolog.TimeFieldFormat = time.RFC3339
	switch strings.ToLower(envDefault("LOG_LEVEL", "info")) {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	cfg := mustConfig()
	ctx := context.Background()

	srv, err := newServer(ctx, cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("init failed")
	}
	server := newMCPServer(srv)

	if isStdio {
		runStdio(ctx, srv, server)
		return
	}

	// --- Streamable HTTP transport ---
	addr := envDefault("HTTP_ADDR", ":8080")
	path := envDefault("HTTP_PATH", "/mcp")
	bearer := strings.TrimSpace(os.Getenv("AUTH_BEARER"))

	base := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return server }, nil)
	var handler http.Handler = base
	if bearer != "" {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if strings.TrimSpace(got) != bearer {
				auditLog("auth_failed", r.RemoteAddr, "", "invalid bearer token", false)
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("unauthorized"))
				return
			}
			base.ServeHTTP(w, r)
		})
	}

	// Apply middleware
	handler = requestSizeLimitMiddleware(handler)

	mux := http.NewServeMux()
	mux.Handle(path, handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	})

	// Create HTTP server
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	srv.server = httpServer

	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Info().Msg("received shutdown signal")

		// Give ongoing requests 30 seconds to complete
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("error during shutdown")
		}
		os.Exit(0)
	}()

	log.Info().Str("addr", addr).Str("path", path).Msg("starting MCP server on streamable HTTP")
	auditLog("server_start", "system", "", addr, true)

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal().Err(err).Msg("server error")
	}
}

// runStdio serves MCP over stdin/stdout (mssql-mcp style: {"command": ".../postgres-mcp-server", "env": {...}}).
// stdout is reserved for the protocol — all logs must go to stderr (configured in main).
func runStdio(ctx context.Context, srv *Server, server *mcp.Server) {
	defer func() {
		if err := srv.Shutdown(context.Background()); err != nil {
			log.Error().Err(err).Msg("stdio server shutdown failed")
		}
	}()

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info().Msg("starting MCP server on stdio")
	auditLog("server_start", "system", "", "stdio", true)

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatal().Err(err).Msg("stdio server error")
	}
}
