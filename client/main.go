// client/main.go
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"maps"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Build-time variables (set via ldflags)
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type queriesFlag []string

func (q *queriesFlag) String() string     { return strings.Join(*q, "; ") }
func (q *queriesFlag) Set(v string) error { *q = append(*q, v); return nil }

func main() {
	url := getenv("POSTGRES_MCP_SERVER_URL", "http://127.0.0.1:8080/mcp")
	bearer := os.Getenv("POSTGRES_MCP_AUTH_BEARER")

	serverURL := flag.String("url", url, "MCP server URL (e.g. http://host:8080/mcp)")
	auth := flag.String("bearer", bearer, "Optional bearer token")
	format := flag.String("format", "json", "Output format: table, json, csv")
	verbose := flag.Bool("verbose", false, "Verbose output")
	maxRows := flag.Int("max-rows", 1000, "Maximum rows to return from each query")
	versionFlag := flag.Bool("version", false, "Print version information and exit")
	var queries queriesFlag
	flag.Var(&queries, "query", "SQL query to run (repeatable)")
	search := flag.String("search", "", "Optional free-text search string")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("postgres-mcp-client %s\n", version)
		fmt.Printf("  commit: %s\n", commit)
		fmt.Printf("  built:  %s\n", date)
		os.Exit(0)
	}

	// Validate format
	validFormats := map[string]bool{"table": true, "json": true, "csv": true}
	if !validFormats[*format] {
		log.Fatalf("Invalid format '%s', must be one of: table, json, csv", *format)
	}

	httpClient := &http.Client{
		Transport: &authRoundTripper{base: http.DefaultTransport, bearer: strings.TrimSpace(*auth)},
	}
	tr := &mcp.StreamableClientTransport{
		Endpoint:   *serverURL,
		HTTPClient: httpClient,
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "postgres-mcp-client", Version: "0.5.0"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, tr, nil)
	if err != nil {
		log.Fatalf("connect to %s failed: %v", *serverURL, err)
	}
	defer session.Close()

	if _, err := session.ListTools(ctx, &mcp.ListToolsParams{}); err != nil {
		log.Fatalf("tools/list failed: %v", err)
	}

	if *verbose {
		fmt.Printf("Connected to server at %s\n", *serverURL)
	}

	for _, sql := range queries {
		if *verbose {
			fmt.Printf("Running SQL: %s\n", sql)
		}
		runQuery(ctx, session, sql, *format, *verbose, *maxRows)
	}
	if s := strings.TrimSpace(*search); s != "" {
		if *verbose {
			fmt.Printf("Searching for: %s\n", s)
		}
		runSearch(ctx, session, s, *format, *verbose)
	}
}

func runQuery(ctx context.Context, session *mcp.ClientSession, sql, format string, verbose bool, maxRows int) {
	call(ctx, session, "query", map[string]any{"sql": sql, "max_rows": maxRows}, format, verbose)
}

func runSearch(ctx context.Context, session *mcp.ClientSession, q, format string, verbose bool) {
	args := map[string]any{"q": q, "limit": 50}
	call(ctx, session, "search", args, format, verbose)
}

func call(ctx context.Context, session *mcp.ClientSession, tool string, args map[string]any, format string, verbose bool) {
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		log.Fatalf("%s failed: %v", tool, err)
	}
	if res.IsError {
		printContent(res.Content)
		log.Fatalf("%s returned error", tool)
	}

	// The server sends its result once, as JSON text, rather than as structured content.
	if len(res.Content) == 1 {
		if text, ok := res.Content[0].(*mcp.TextContent); ok {
			var result map[string]any
			if err := json.Unmarshal([]byte(text.Text), &result); err == nil {
				printFormattedResult(result, format, verbose)
				return
			}
		}
	}
	printContent(res.Content)
}

func printFormattedResult(result map[string]any, format string, verbose bool) {
	// Extract rows if present
	rows, hasRows := result["rows"].([]any)
	// Rows arrive as arrays in "columns" order; the printers take one map per row.
	if columns, ok := result["columns"].([]any); ok {
		for i, row := range rows {
			values, _ := row.([]any)
			record := make(map[string]any, len(columns))
			for j, column := range columns {
				if j < len(values) {
					record[fmt.Sprint(column)] = values[j]
				}
			}
			rows[i] = record
		}
	}
	if truncated, _ := result["truncated"].(bool); truncated {
		fmt.Fprintf(os.Stderr, "Results truncated after %d rows\n", len(rows))
	}

	if !hasRows || len(rows) == 0 {
		if format == "json" {
			printJSON(result)
			return
		}
		fmt.Println("No results found.")
		return
	}

	if verbose && len(rows) > 0 {
		fmt.Printf("Showing %d results\n\n", len(rows))
	}

	switch format {
	case "table":
		printTable(rows)
	case "csv":
		printCSV(rows)
	case "json":
		printJSON(result)
	default:
		printJSON(result)
	}
}

func printTable(rows []any) {
	if len(rows) == 0 {
		return
	}

	// Convert to []map[string]any
	var records []map[string]any
	for _, row := range rows {
		if record, ok := row.(map[string]any); ok {
			records = append(records, record)
		}
	}

	if len(records) == 0 {
		return
	}

	// Get all column names
	columnSet := make(map[string]bool)
	for _, record := range records {
		for key := range record {
			columnSet[key] = true
		}
	}

	// Sort column names for consistent output
	columns := slices.Sorted(maps.Keys(columnSet))

	// Create table writer
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	// Print header
	for i, col := range columns {
		if i > 0 {
			fmt.Fprint(w, "\t")
		}
		fmt.Fprint(w, strings.ToUpper(col))
	}
	fmt.Fprintln(w)

	// Print separator
	for i := range columns {
		if i > 0 {
			fmt.Fprint(w, "\t")
		}
		fmt.Fprint(w, strings.Repeat("-", len(columns[i])+2))
	}
	fmt.Fprintln(w)

	// Print rows
	for _, record := range records {
		for i, col := range columns {
			if i > 0 {
				fmt.Fprint(w, "\t")
			}
			value := record[col]
			fmt.Fprint(w, formatValue(value))
		}
		fmt.Fprintln(w)
	}

	fmt.Printf("\n(%d rows)\n", len(records))
	if err := w.Flush(); err != nil {
		log.Printf("Error flushing table output: %v", err)
	}
}

func printCSV(rows []any) {
	if len(rows) == 0 {
		return
	}

	// Convert to []map[string]any
	var records []map[string]any
	for _, row := range rows {
		if record, ok := row.(map[string]any); ok {
			records = append(records, record)
		}
	}

	if len(records) == 0 {
		return
	}

	// Get all column names
	columnSet := make(map[string]bool)
	for _, record := range records {
		for key := range record {
			columnSet[key] = true
		}
	}

	// Sort column names for consistent output
	columns := slices.Sorted(maps.Keys(columnSet))

	// Create CSV writer
	writer := csv.NewWriter(os.Stdout)
	defer writer.Flush()

	// Write header
	if err := writer.Write(columns); err != nil {
		log.Printf("Error writing CSV header: %v", err)
	}

	// Write rows
	for _, record := range records {
		var row []string
		for _, col := range columns {
			value := record[col]
			row = append(row, formatValue(value))
		}
		if err := writer.Write(row); err != nil {
			log.Printf("Error writing CSV row: %v", err)
		}
	}
}

func printJSON(data any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		log.Printf("Error encoding JSON: %v", err)
	}
}

func formatValue(value any) string {
	if value == nil {
		return ""
	}

	switch v := value.(type) {
	case string:
		return v
	case int, int32, int64:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%.2f", v)
	case bool:
		return strconv.FormatBool(v)
	case time.Time:
		return v.Format("2006-01-02 15:04:05")
	default:
		// For complex types, try JSON marshaling
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", v)
	}
}

func printContent(cs []mcp.Content) {
	for _, c := range cs {
		switch v := c.(type) {
		case *mcp.TextContent:
			if pretty, ok := tryPrettyJSON(v.Text); ok {
				fmt.Println(pretty)
			} else {
				fmt.Println(v.Text)
			}
		case *mcp.ImageContent:
			out := map[string]any{"type": "image", "mimeType": v.MIMEType}
			b, _ := json.MarshalIndent(out, "", "  ")
			fmt.Println(string(b))
		default:
			b, _ := json.MarshalIndent(v, "", "  ")
			fmt.Println(string(b))
		}
	}
}

type authRoundTripper struct {
	base   http.RoundTripper
	bearer string
}

func (rt *authRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	if rt.bearer != "" {
		r.Header.Set("Authorization", "Bearer "+rt.bearer)
	}
	return rt.base.RoundTrip(r)
}

func tryPrettyJSON(s string) (string, bool) {
	var anyJSON any
	if err := json.Unmarshal([]byte(s), &anyJSON); err != nil {
		return "", false
	}
	b, _ := json.MarshalIndent(anyJSON, "", "  ")
	return string(b), true
}
func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
