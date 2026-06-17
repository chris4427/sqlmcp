// Package tools registers MCP tools for interacting with a SQL database.
package tools

import (
	"database/sql"
	"strings"

	"github.com/mark3labs/mcp-go/server"
)

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

// RegisterAll registers all SQL tools with the MCP server, selecting
// driver-appropriate variants where needed.
func RegisterAll(s *server.MCPServer, db *sql.DB, driverName string, cfg Config) {
	registerQuery(s, db, cfg)
	registerExecStatement(s, db)
	registerBenchmarkQuery(s, db)
	registerCompareQueries(s, db)

	if strings.EqualFold(driverName, "sqlite") {
		registerSQLiteDescribeTable(s, db)
	} else {
		registerDescribeTable(s, db)
	}
}
