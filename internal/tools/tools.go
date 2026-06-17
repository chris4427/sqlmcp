// Package tools registers MCP tools for interacting with a SQL database.
package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ---------------------------------------------------------------------------
// Tool: query
// ---------------------------------------------------------------------------

const (
	defaultRowLimit   = 100
	defaultValueLimit = 500
	maxRowLimit       = 1000
	maxValueLimit     = 10000
)

func registerQuery(s *server.MCPServer, db *sql.DB) {
	tool := mcp.NewTool("query",
		mcp.WithDescription(
			"Execute a SQL query and return the results as JSON. "+
				"Use for SELECT statements or any query that returns rows. "+
				"For INSERT/UPDATE/DELETE use exec_statement instead. "+
				fmt.Sprintf("Defaults to %d rows max and %d chars per value to keep responses compact.", defaultRowLimit, defaultValueLimit),
		),
		mcp.WithString("sql",
			mcp.Required(),
			mcp.Description("The SQL query to execute (SELECT, SHOW, EXPLAIN, etc.)"),
		),
		mcp.WithNumber("row_limit",
			mcp.Description(fmt.Sprintf(
				"Maximum number of rows to return (default %d, max %d). Use a higher value if you need more.",
				defaultRowLimit, maxRowLimit,
			)),
		),
		mcp.WithNumber("value_limit",
			mcp.Description(fmt.Sprintf(
				"Maximum characters per cell value (default %d, max %d). Longer values are truncated with '[truncated]'.",
				defaultValueLimit, maxValueLimit,
			)),
		),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := req.RequireString("sql")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		rowLimit := clampInt(int(req.GetFloat("row_limit", defaultRowLimit)), 1, maxRowLimit)
		valueLimit := clampInt(int(req.GetFloat("value_limit", defaultValueLimit)), 1, maxValueLimit)

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

		query := buildDescribeQuery(table, schema)

		rows, err := db.QueryContext(ctx, query)
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

func buildDescribeQuery(table, schema string) string {
	schemaFilter := ""
	if schema != "" {
		schemaFilter = fmt.Sprintf(" AND TABLE_SCHEMA = '%s'", schema)
	}

	return fmt.Sprintf(
		"SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_DEFAULT, COLUMN_KEY "+
			"FROM INFORMATION_SCHEMA.COLUMNS "+
			"WHERE TABLE_NAME = '%s'%s "+
			"ORDER BY ORDINAL_POSITION",
		// Sanitise table name: strip single quotes to prevent basic injection.
		strings.ReplaceAll(table, "'", "''"),
		schemaFilter,
	)
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

		// SQLite PRAGMA table_info returns: cid, name, type, notnull, dflt_value, pk
		rows, err := db.QueryContext(ctx,
			fmt.Sprintf("PRAGMA table_info(%q)", table),
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
// RegisterWithDriver chooses the correct tool variants based on driver name.
// ---------------------------------------------------------------------------

// RegisterAll registers all tools, selecting driver-appropriate variants.
func RegisterAll(s *server.MCPServer, db *sql.DB, driverName string) {
	registerQuery(s, db)
	registerExecStatement(s, db)
	registerBenchmarkQuery(s, db)

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
