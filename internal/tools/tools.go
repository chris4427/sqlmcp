// Package tools registers MCP tools for interacting with a SQL database.
package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ---------------------------------------------------------------------------
// Tool: query
// ---------------------------------------------------------------------------

const (
	DefaultRowLimit   = 100
	DefaultValueLimit = 500
	maxRowLimit       = 1000
	maxValueLimit     = 10000
)

// Config holds server-wide defaults for query output limits.
// Individual tool calls may override these per-request.
type Config struct {
	DefaultRowLimit   int
	DefaultValueLimit int
}

func registerQuery(s *server.MCPServer, db *sql.DB, cfg Config) {
	tool := mcp.NewTool("query",
		mcp.WithDescription(
			"Execute a SQL query and return the results as JSON. "+
				"Use for SELECT statements or any query that returns rows. "+
				"For INSERT/UPDATE/DELETE use exec_statement instead. "+
				fmt.Sprintf("Defaults to %d rows max and %d chars per value to keep responses compact.", cfg.DefaultRowLimit, cfg.DefaultValueLimit),
		),
		mcp.WithString("sql",
			mcp.Required(),
			mcp.Description("The SQL query to execute (SELECT, SHOW, EXPLAIN, etc.)"),
		),
		mcp.WithNumber("row_limit",
			mcp.Description(fmt.Sprintf(
				"Maximum number of rows to return (default %d, max %d). Use a higher value if you need more.",
				cfg.DefaultRowLimit, maxRowLimit,
			)),
		),
		mcp.WithNumber("value_limit",
			mcp.Description(fmt.Sprintf(
				"Maximum characters per cell value (default %d, max %d). Longer values are truncated with '[truncated]'.",
				cfg.DefaultValueLimit, maxValueLimit,
			)),
		),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := req.RequireString("sql")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		rowLimit := clampInt(int(req.GetFloat("row_limit", float64(cfg.DefaultRowLimit))), 1, maxRowLimit)
		valueLimit := clampInt(int(req.GetFloat("value_limit", float64(cfg.DefaultValueLimit))), 1, maxValueLimit)

		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("query error: %v", err)), nil
		}
		defer rows.Close()

		result, err := rowsToJSON(rows, rowLimit, valueLimit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("result encoding error: %v", err)), nil
		}

		return mcp.NewToolResultText(result), nil
	})
}

// ---------------------------------------------------------------------------
// Tool: exec_statement
// ---------------------------------------------------------------------------

func registerExecStatement(s *server.MCPServer, db *sql.DB) {
	tool := mcp.NewTool("exec_statement",
		mcp.WithDescription(
			"Execute a SQL statement that does not return rows "+
				"(INSERT, UPDATE, DELETE, CREATE, DROP, ALTER, etc.). "+
				"Returns rows affected and last insert ID where supported.",
		),
		mcp.WithString("sql",
			mcp.Required(),
			mcp.Description("The SQL statement to execute"),
		),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		stmt, err := req.RequireString("sql")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		res, err := db.ExecContext(ctx, stmt)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("exec error: %v", err)), nil
		}

		rowsAffected, _ := res.RowsAffected()
		lastInsertID, _ := res.LastInsertId()

		out := fmt.Sprintf(`{"rows_affected":%d,"last_insert_id":%d}`, rowsAffected, lastInsertID)
		return mcp.NewToolResultText(out), nil
	})
}

// ---------------------------------------------------------------------------
// Tool: benchmark_query
// ---------------------------------------------------------------------------

func registerBenchmarkQuery(s *server.MCPServer, db *sql.DB) {
	tool := mcp.NewTool("benchmark_query",
		mcp.WithDescription(
			"Execute a SQL query multiple times and return timing statistics: "+
				"min, max, mean, and total duration in milliseconds, plus the row count. "+
				"Useful for measuring query performance and spotting variance.",
		),
		mcp.WithString("sql",
			mcp.Required(),
			mcp.Description("The SQL query to benchmark"),
		),
		mcp.WithNumber("runs",
			mcp.Description("Number of times to run the query (default 10, max 100)"),
		),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := req.RequireString("sql")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		runs := int(req.GetFloat("runs", 10))
		if runs < 1 {
			runs = 1
		}
		if runs > 100 {
			runs = 100
		}

		var durations []time.Duration
		var rowCount int

		for i := 0; i < runs; i++ {
			start := time.Now()

			rows, err := db.QueryContext(ctx, query)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("query error on run %d: %v", i+1, err)), nil
			}

			// Drain rows so the query fully executes server-side.
			n := 0
			for rows.Next() {
				n++
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("row error on run %d: %v", i+1, err)), nil
			}

			durations = append(durations, time.Since(start))

			// Only record row count from the first run.
			if i == 0 {
				rowCount = n
			}
		}

		// Compute stats.
		var total time.Duration
		min, max := durations[0], durations[0]
		for _, d := range durations {
			total += d
			if d < min {
				min = d
			}
			if d > max {
				max = d
			}
		}
		mean := total / time.Duration(runs)

		type benchResult struct {
			Runs     int     `json:"runs"`
			RowCount int     `json:"row_count"`
			MinMs    float64 `json:"min_ms"`
			MaxMs    float64 `json:"max_ms"`
			MeanMs   float64 `json:"mean_ms"`
			TotalMs  float64 `json:"total_ms"`
		}

		r := benchResult{
			Runs:     runs,
			RowCount: rowCount,
			MinMs:    float64(min.Microseconds()) / 1000,
			MaxMs:    float64(max.Microseconds()) / 1000,
			MeanMs:   float64(mean.Microseconds()) / 1000,
			TotalMs:  float64(total.Microseconds()) / 1000,
		}

		b, err := json.Marshal(r)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("encoding error: %v", err)), nil
		}

		return mcp.NewToolResultText(string(b)), nil
	})
}

// ---------------------------------------------------------------------------
// Tool: describe_table (standard)
// ---------------------------------------------------------------------------

func registerDescribeTable(s *server.MCPServer, db *sql.DB) {
	tool := mcp.NewTool("describe_table",
		mcp.WithDescription(
			"Describe the columns of a table: name, data type, nullable, default, and key info.",
		),
		mcp.WithString("table",
			mcp.Required(),
			mcp.Description("Name of the table to describe"),
		),
		mcp.WithString("schema",
			mcp.Description(
				"Optional: schema/database that owns the table. "+
					"Leave empty to use the default schema.",
			),
		),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		table, err := req.RequireString("table")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		schema := req.GetString("schema", "")

		q, args := buildDescribeQuery(table, schema)

		rows, err := db.QueryContext(ctx, q, args...)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("describe_table error: %v", err)), nil
		}
		defer rows.Close()

		result, err := rowsToJSON(rows, maxRowLimit, maxValueLimit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("result encoding error: %v", err)), nil
		}

		return mcp.NewToolResultText(result), nil
	})
}

// buildDescribeQuery returns a parameterized INFORMATION_SCHEMA query and its
// arguments. Using bound parameters avoids any issues with special characters
// in table or schema names (e.g. apostrophes).
func buildDescribeQuery(table, schema string) (string, []any) {
	if schema != "" {
		return "SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_DEFAULT, COLUMN_KEY " +
			"FROM INFORMATION_SCHEMA.COLUMNS " +
			"WHERE TABLE_NAME = ? AND TABLE_SCHEMA = ? " +
			"ORDER BY ORDINAL_POSITION", []any{table, schema}
	}
	return "SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_DEFAULT, COLUMN_KEY " +
		"FROM INFORMATION_SCHEMA.COLUMNS " +
		"WHERE TABLE_NAME = ? " +
		"ORDER BY ORDINAL_POSITION", []any{table}
}

// ---------------------------------------------------------------------------
// Tool: describe_table (SQLite variant)
// ---------------------------------------------------------------------------

func RegisterSQLiteDescribeTable(s *server.MCPServer, db *sql.DB) {
	tool := mcp.NewTool("describe_table",
		mcp.WithDescription("Describe the columns of a SQLite table."),
		mcp.WithString("table",
			mcp.Required(),
			mcp.Description("Name of the table to describe"),
		),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		table, err := req.RequireString("table")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Validate the table name exists in sqlite_master via a parameterized
		// query before passing it to PRAGMA, which doesn't support bound params.
		var resolvedName string
		err = db.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type IN ('table','view') AND name = ?",
			table,
		).Scan(&resolvedName)
		if err == sql.ErrNoRows {
			return mcp.NewToolResultError(fmt.Sprintf("table %q not found", table)), nil
		}
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("describe_table error: %v", err)), nil
		}

		// resolvedName came from the database itself, not the user, so it is
		// safe to interpolate into the PRAGMA statement.
		rows, err := db.QueryContext(ctx,
			fmt.Sprintf("PRAGMA table_info(%q)", resolvedName),
		)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("describe_table error: %v", err)), nil
		}
		defer rows.Close()

		result, err := rowsToJSON(rows, maxRowLimit, maxValueLimit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("result encoding error: %v", err)), nil
		}

		return mcp.NewToolResultText(result), nil
	})
}

// ---------------------------------------------------------------------------
// Tool: compare_queries
// ---------------------------------------------------------------------------

func registerCompareQueries(s *server.MCPServer, db *sql.DB) {
	tool := mcp.NewTool("compare_queries",
		mcp.WithDescription(`Compare two SQL queries (or stored procedure calls) to verify they return identical results across a set of parameter combinations.

Use this tool to validate that a rewritten or optimized query is semantically equivalent to the original.

IMPORTANT — before calling this tool, gather good test parameters:
1. Call describe_table on every table involved to understand column types and nullability.
2. Call query to sample real data for columns used in WHERE clauses or as parameters:
   - SELECT DISTINCT <col> FROM <table> ORDER BY <col> — for categorical/enum-like columns
   - SELECT MIN(<col>), MAX(<col>) FROM <table> — for numeric/date ranges
   - SELECT <col> FROM <table> WHERE <col> IS NULL LIMIT 1 — to confirm NULLs exist
3. Build parameter sets that cover:
   - Common/frequent values from the real data
   - Boundary values (min, max of ranges)
   - NULL for every nullable parameter
   - Zero, empty string, and negative numbers where the type allows
   - At least 10 parameter sets for high confidence; more is better

Placeholders: use {{name}} syntax in both queries. Example:
  query1: "SELECT * FROM orders WHERE user_id = {{user_id}} AND status = {{status}}"
  params: [{"user_id": 1, "status": "open"}, {"user_id": 2, "status": null}]

Comparison is order-insensitive: rows and columns are sorted before diffing so
minor ordering differences do not produce false failures.`),
		mcp.WithString("query1",
			mcp.Required(),
			mcp.Description("The original query or CALL/EXEC statement. Use {{param_name}} placeholders for parameters."),
		),
		mcp.WithString("query2",
			mcp.Required(),
			mcp.Description("The rewritten/optimized query to compare against query1. Must use the same {{param_name}} placeholders."),
		),
		mcp.WithArray("params",
			mcp.Required(),
			mcp.Description("Array of parameter objects. Each object maps placeholder names to values. Example: [{\"user_id\": 1, \"status\": \"open\"}, {\"user_id\": 2, \"status\": null}]"),
			mcp.Items(map[string]any{"type": "object"}),
		),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q1, err := req.RequireString("query1")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		q2, err := req.RequireString("query2")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Parse the params array — it comes in as []any where each element is
		// a map[string]any.
		rawParams, ok := req.GetArguments()["params"]
		if !ok {
			return mcp.NewToolResultError("params is required"), nil
		}
		paramSlice, ok := rawParams.([]any)
		if !ok {
			return mcp.NewToolResultError("params must be an array of objects"), nil
		}
		if len(paramSlice) == 0 {
			return mcp.NewToolResultError("params must contain at least one parameter set"), nil
		}

		type runResult struct {
			ParamSet int            `json:"param_set"`
			Params   map[string]any `json:"params"`
			Match    bool           `json:"match"`
			// Only populated on mismatch or error.
			Query1Rows int    `json:"query1_rows,omitempty"`
			Query2Rows int    `json:"query2_rows,omitempty"`
			Diff       string `json:"diff,omitempty"`
			Error      string `json:"error,omitempty"`
		}

		var results []runResult
		allMatch := true

		for i, p := range paramSlice {
			params, ok := p.(map[string]any)
			if !ok {
				return mcp.NewToolResultError(fmt.Sprintf("params[%d] must be an object", i)), nil
			}

			r1, err1 := runAndNormalize(ctx, db, q1, params)
			r2, err2 := runAndNormalize(ctx, db, q2, params)

			res := runResult{
				ParamSet: i + 1,
				Params:   params,
			}

			if err1 != nil || err2 != nil {
				allMatch = false
				res.Match = false
				switch {
				case err1 != nil && err2 != nil:
					res.Error = fmt.Sprintf("query1 error: %v | query2 error: %v", err1, err2)
				case err1 != nil:
					res.Error = fmt.Sprintf("query1 error: %v", err1)
				default:
					res.Error = fmt.Sprintf("query2 error: %v", err2)
				}
			} else if r1 != r2 {
				allMatch = false
				res.Match = false
				res.Query1Rows = strings.Count(r1, "\n") + 1
				res.Query2Rows = strings.Count(r2, "\n") + 1
				res.Diff = buildDiff(r1, r2)
			} else {
				res.Match = true
			}

			results = append(results, res)
		}

		type response struct {
			Match       bool        `json:"match"`
			TotalSets   int         `json:"total_sets"`
			PassedSets  int         `json:"passed_sets"`
			FailedSets  int         `json:"failed_sets"`
			Results     []runResult `json:"results"`
		}

		passed := 0
		for _, r := range results {
			if r.Match {
				passed++
			}
		}

		// Only include failed results in the output to keep it compact —
		// the AI doesn't need to see every passing run.
		var filteredResults []runResult
		for _, r := range results {
			if !r.Match {
				filteredResults = append(filteredResults, r)
			}
		}
		if filteredResults == nil {
			filteredResults = []runResult{}
		}

		b, err := json.Marshal(response{
			Match:      allMatch,
			TotalSets:  len(results),
			PassedSets: passed,
			FailedSets: len(results) - passed,
			Results:    filteredResults,
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("encoding error: %v", err)), nil
		}

		return mcp.NewToolResultText(string(b)), nil
	})
}

// runAndNormalize executes a query with placeholders substituted, drains the
// rows, and returns a canonical string representation suitable for equality
// comparison. Rows and columns are both sorted so order does not matter.
func runAndNormalize(ctx context.Context, db *sql.DB, query string, params map[string]any) (string, error) {
	q := substitutePlaceholders(query, params)

	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}

	// Sort columns alphabetically so column order doesn't matter.
	sortedCols := make([]string, len(columns))
	copy(sortedCols, columns)
	colIndex := make(map[string]int, len(columns))
	for i, c := range columns {
		colIndex[c] = i
	}
	sortStrings(sortedCols)

	var rowStrings []string

	for rows.Next() {
		vals := make([]any, len(columns))
		valPtrs := make([]any, len(columns))
		for i := range vals {
			valPtrs[i] = &vals[i]
		}
		if err := rows.Scan(valPtrs...); err != nil {
			return "", err
		}

		// Build a sorted-column representation of this row.
		parts := make([]string, len(sortedCols))
		for i, col := range sortedCols {
			v := vals[colIndex[col]]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			parts[i] = fmt.Sprintf("%s=%v", col, v)
		}
		rowStrings = append(rowStrings, strings.Join(parts, ","))
	}

	if err := rows.Err(); err != nil {
		return "", err
	}

	// Sort rows so result order doesn't matter.
	sortStrings(rowStrings)
	return strings.Join(rowStrings, "\n"), nil
}

// substitutePlaceholders replaces {{name}} tokens in query with the
// corresponding value from params. nil values become NULL.
//
// String values have single quotes doubled, which is standard SQL escaping and
// works correctly on PostgreSQL, SQLite, and SQL Server. MySQL requires
// NO_BACKSLASH_ESCAPES mode to be set for this to be fully safe with values
// that contain backslashes — if your MySQL data contains backslashes, set that
// mode on the connection.
func substitutePlaceholders(query string, params map[string]any) string {
	for k, v := range params {
		var replacement string
		if v == nil {
			replacement = "NULL"
		} else {
			switch val := v.(type) {
			case string:
				replacement = "'" + strings.ReplaceAll(val, "'", "''") + "'"
			case bool:
				if val {
					replacement = "TRUE"
				} else {
					replacement = "FALSE"
				}
			case float64:
				// Guard against non-finite values that are not valid SQL literals.
				if math.IsNaN(val) || math.IsInf(val, 0) {
					replacement = "NULL"
				} else if val == math.Trunc(val) && !math.IsInf(val, 0) {
					// Render whole numbers without a decimal point to avoid
					// float formatting issues like 1e+06.
					replacement = fmt.Sprintf("%d", int64(val))
				} else {
					replacement = fmt.Sprintf("%g", val)
				}
			case int:
				replacement = fmt.Sprintf("%d", val)
			case int64:
				replacement = fmt.Sprintf("%d", val)
			default:
				replacement = fmt.Sprintf("%v", val)
			}
		}
		query = strings.ReplaceAll(query, "{{"+k+"}}", replacement)
	}
	return query
}

// buildDiff returns a human-readable summary of where two normalized result
// strings diverge, capped to keep output compact.
func buildDiff(a, b string) string {
	aLines := strings.Split(a, "\n")
	bLines := strings.Split(b, "\n")

	aSet := make(map[string]int)
	bSet := make(map[string]int)
	for _, l := range aLines {
		if l != "" {
			aSet[l]++
		}
	}
	for _, l := range bLines {
		if l != "" {
			bSet[l]++
		}
	}

	var onlyInA, onlyInB []string
	for l, n := range aSet {
		if bSet[l] < n {
			onlyInA = append(onlyInA, l)
		}
	}
	for l, n := range bSet {
		if aSet[l] < n {
			onlyInB = append(onlyInB, l)
		}
	}
	sortStrings(onlyInA)
	sortStrings(onlyInB)

	const maxLines = 5
	var parts []string
	parts = append(parts, fmt.Sprintf("query1 returned %d rows, query2 returned %d rows.", len(aLines), len(bLines)))

	if len(onlyInA) > 0 {
		sample := onlyInA
		if len(sample) > maxLines {
			sample = sample[:maxLines]
		}
		parts = append(parts, fmt.Sprintf("Rows only in query1 (showing %d of %d): %s",
			len(sample), len(onlyInA), strings.Join(sample, " | ")))
	}
	if len(onlyInB) > 0 {
		sample := onlyInB
		if len(sample) > maxLines {
			sample = sample[:maxLines]
		}
		parts = append(parts, fmt.Sprintf("Rows only in query2 (showing %d of %d): %s",
			len(sample), len(onlyInB), strings.Join(sample, " | ")))
	}

	return strings.Join(parts, " ")
}

// sortStrings sorts a string slice in place.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// ---------------------------------------------------------------------------
// RegisterWithDriver chooses the correct tool variants based on driver name.
// ---------------------------------------------------------------------------

// RegisterAll registers all tools, selecting driver-appropriate variants.
func RegisterAll(s *server.MCPServer, db *sql.DB, driverName string, cfg Config) {
	registerQuery(s, db, cfg)
	registerExecStatement(s, db)
	registerBenchmarkQuery(s, db)
	registerCompareQueries(s, db)

	if strings.EqualFold(driverName, "sqlite") {
		RegisterSQLiteDescribeTable(s, db)
	} else {
		registerDescribeTable(s, db)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// rowsToJSON converts sql.Rows to a JSON object containing the result rows,
// a row count, and a truncated flag if either the row limit or value limit
// was hit.
func rowsToJSON(rows *sql.Rows, rowLimit, valueLimit int) (string, error) {
	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}

	var results []map[string]any
	truncatedRows := false

	for rows.Next() {
		if len(results) >= rowLimit {
			truncatedRows = true
			// Drain remaining rows so the connection is released cleanly.
			for rows.Next() {
			}
			break
		}

		vals := make([]any, len(columns))
		valPtrs := make([]any, len(columns))
		for i := range vals {
			valPtrs[i] = &vals[i]
		}

		if err := rows.Scan(valPtrs...); err != nil {
			return "", err
		}

		row := make(map[string]any, len(columns))
		for i, col := range columns {
			v := vals[i]
			// Convert []byte to string for readability.
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			// Truncate long string values.
			if s, ok := v.(string); ok && len(s) > valueLimit {
				v = s[:valueLimit] + "...[truncated]"
			}
			row[col] = v
		}

		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return "", err
	}

	if results == nil {
		results = []map[string]any{}
	}

	type response struct {
		Rows      []map[string]any `json:"rows"`
		RowCount  int              `json:"row_count"`
		Truncated bool             `json:"truncated,omitempty"`
	}

	b, err := json.Marshal(response{
		Rows:      results,
		RowCount:  len(results),
		Truncated: truncatedRows,
	})
	if err != nil {
		return "", err
	}

	return string(b), nil
}
