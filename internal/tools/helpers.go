package tools

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// clampInt returns v clamped to [min, max].
func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// jsonMarshal is a thin wrapper around json.Marshal for consistent error handling.
func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
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
				} else if val == math.Trunc(val) {
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

// sortStrings sorts a string slice in place using insertion sort.
// Suitable for the small slices produced by query results.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
