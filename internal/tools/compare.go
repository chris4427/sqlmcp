package tools

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

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

		// Validate all param sets up front before spawning goroutines.
		paramMaps := make([]map[string]any, len(paramSlice))
		for i, p := range paramSlice {
			params, ok := p.(map[string]any)
			if !ok {
				return mcp.NewToolResultError(fmt.Sprintf("params[%d] must be an object", i)), nil
			}
			paramMaps[i] = params
		}

		type runResult struct {
			ParamSet   int            `json:"param_set"`
			Params     map[string]any `json:"params"`
			Match      bool           `json:"match"`
			Query1Rows int            `json:"query1_rows,omitempty"`
			Query2Rows int            `json:"query2_rows,omitempty"`
			Diff       string         `json:"diff,omitempty"`
			Error      string         `json:"error,omitempty"`
		}

		// Run all param sets concurrently. Cap at 10 concurrent DB round-trips
		// to avoid overwhelming the connection pool.
		const maxConcurrent = 10
		sem := make(chan struct{}, maxConcurrent)
		results := make([]runResult, len(paramMaps))
		var wg sync.WaitGroup

		for i, params := range paramMaps {
			wg.Add(1)
			go func(i int, params map[string]any) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				res := runResult{ParamSet: i + 1, Params: params}

				// Run query1 and query2 concurrently within this param set.
				type queryResult struct {
					out string
					err error
				}
				ch1 := make(chan queryResult, 1)
				ch2 := make(chan queryResult, 1)

				go func() {
					out, err := runAndNormalize(ctx, db, q1, params)
					ch1 <- queryResult{out, err}
				}()
				go func() {
					out, err := runAndNormalize(ctx, db, q2, params)
					ch2 <- queryResult{out, err}
				}()

				qr1 := <-ch1
				qr2 := <-ch2

				if qr1.err != nil || qr2.err != nil {
					res.Match = false
					switch {
					case qr1.err != nil && qr2.err != nil:
						res.Error = fmt.Sprintf("query1 error: %v | query2 error: %v", qr1.err, qr2.err)
					case qr1.err != nil:
						res.Error = fmt.Sprintf("query1 error: %v", qr1.err)
					default:
						res.Error = fmt.Sprintf("query2 error: %v", qr2.err)
					}
				} else if qr1.out != qr2.out {
					res.Match = false
					res.Query1Rows = strings.Count(qr1.out, "\n") + 1
					res.Query2Rows = strings.Count(qr2.out, "\n") + 1
					res.Diff = buildDiff(qr1.out, qr2.out)
				} else {
					res.Match = true
				}

				results[i] = res
			}(i, params)
		}

		wg.Wait()

		type response struct {
			Match      bool        `json:"match"`
			TotalSets  int         `json:"total_sets"`
			PassedSets int         `json:"passed_sets"`
			FailedSets int         `json:"failed_sets"`
			Results    []runResult `json:"results"`
		}

		passed := 0
		for _, r := range results {
			if r.Match {
				passed++
			}
		}

		// Only include failed results — the AI doesn't need to see every passing run.
		var failedResults []runResult
		for _, r := range results {
			if !r.Match {
				failedResults = append(failedResults, r)
			}
		}
		if failedResults == nil {
			failedResults = []runResult{}
		}

		b, err := jsonMarshal(response{
			Match:      passed == len(results),
			TotalSets:  len(results),
			PassedSets: passed,
			FailedSets: len(results) - passed,
			Results:    failedResults,
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

	sortStrings(rowStrings)
	return strings.Join(rowStrings, "\n"), nil
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
