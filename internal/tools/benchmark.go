package tools

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

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

		// Runs are intentionally sequential — parallelizing would measure
		// concurrent load rather than individual query latency.
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

		type result struct {
			Runs     int     `json:"runs"`
			RowCount int     `json:"row_count"`
			MinMs    float64 `json:"min_ms"`
			MaxMs    float64 `json:"max_ms"`
			MeanMs   float64 `json:"mean_ms"`
			TotalMs  float64 `json:"total_ms"`
		}

		r := result{
			Runs:     runs,
			RowCount: rowCount,
			MinMs:    float64(min.Microseconds()) / 1000,
			MaxMs:    float64(max.Microseconds()) / 1000,
			MeanMs:   float64(mean.Microseconds()) / 1000,
			TotalMs:  float64(total.Microseconds()) / 1000,
		}

		b, err := jsonMarshal(r)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("encoding error: %v", err)), nil
		}

		return mcp.NewToolResultText(string(b)), nil
	})
}
