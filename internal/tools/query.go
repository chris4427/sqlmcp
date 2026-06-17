package tools

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

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
