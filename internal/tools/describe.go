package tools

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

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

func registerSQLiteDescribeTable(s *server.MCPServer, db *sql.DB) {
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
