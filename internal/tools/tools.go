// Package tools registers MCP tools for interacting with a SQL database.
package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ---------------------------------------------------------------------------
// Tool: query
// ---------------------------------------------------------------------------

func registerQuery(s *server.MCPServer, db *sql.DB) {
	tool := mcp.NewTool("query",
		mcp.WithDescription(
			"Execute a SQL query and return the results as JSON. "+
				"Use for SELECT statements or any query that returns rows. "+
				"For INSERT/UPDATE/DELETE use exec_statement instead.",
		),
		mcp.WithString("sql",
			mcp.Required(),
			mcp.Description("The SQL query to execute (SELECT, SHOW, EXPLAIN, etc.)"),
		),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := req.RequireString("sql")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("query error: %v", err)), nil
		}
		defer rows.Close()

		result, err := rowsToJSON(rows)
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
// Tool: describe_table
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

		result, err := rowsToJSON(rows)
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

		result, err := rowsToJSON(rows)
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

	if strings.EqualFold(driverName, "sqlite") {
		RegisterSQLiteDescribeTable(s, db)
	} else {
		registerDescribeTable(s, db)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// rowsToJSON converts sql.Rows to a JSON array of objects.
func rowsToJSON(rows *sql.Rows) (string, error) {
	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}

	var results []map[string]any

	for rows.Next() {
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

	b, err := json.Marshal(results)
	if err != nil {
		return "", err
	}

	return string(b), nil
}
