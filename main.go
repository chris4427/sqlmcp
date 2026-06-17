package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/mark3labs/mcp-go/server"

	"github.com/cray/sqlmcp/internal/db"
	"github.com/cray/sqlmcp/internal/setup"
	"github.com/cray/sqlmcp/internal/tools"
)

// version is set at build time via -ldflags "-X main.version=<tag>".
// Falls back to "dev" when running outside of a release build.
var version = "dev"

func main() {
	// Handle subcommands before flag parsing.
	if len(os.Args) > 1 && os.Args[1] == "setup" {
		binaryPath, _ := os.Executable()
		if err := setup.Run(binaryPath); err != nil {
			fmt.Fprintf(os.Stderr, "setup error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// ------------------------------------------------------------------
	// Configuration: flags with env-var fallbacks.
	// ------------------------------------------------------------------
	var (
		driverStr  = flag.String("driver", env("SQL_DRIVER", ""), "SQL driver: postgres | mysql | sqlite | sqlserver")
		dsn        = flag.String("dsn", env("SQL_DSN", ""), "Data source name / connection string")
		rowLimit   = flag.Int("row-limit", envInt("SQL_ROW_LIMIT", tools.DefaultRowLimit), "Default max rows returned by the query tool")
		valueLimit = flag.Int("value-limit", envInt("SQL_VALUE_LIMIT", tools.DefaultValueLimit), "Default max characters per cell value in query results")
		showHelp   = flag.Bool("help", false, "Show usage")
	)

	flag.Usage = usage
	flag.Parse()

	if *showHelp {
		usage()
		os.Exit(0)
	}

	// ------------------------------------------------------------------
	// Validate required config.
	// ------------------------------------------------------------------
	if *driverStr == "" {
		fmt.Fprintln(os.Stderr, "error: -driver (or SQL_DRIVER env) is required")
		usage()
		os.Exit(1)
	}
	if *dsn == "" {
		fmt.Fprintln(os.Stderr, "error: -dsn (or SQL_DSN env) is required")
		usage()
		os.Exit(1)
	}

	driver, err := db.ParseDriver(*driverStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// ------------------------------------------------------------------
	// Open database connection.
	// ------------------------------------------------------------------
	sqlDB, err := db.Open(driver, *dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not connect to database: %v\n", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	log.Printf("sqlmcp v%s: connected to %s database", version, driver)

	// ------------------------------------------------------------------
	// Build MCP server.
	// ------------------------------------------------------------------
	s := server.NewMCPServer(
		"sqlmcp",
		version,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)

	tools.RegisterAll(s, sqlDB, string(driver), tools.Config{
		DefaultRowLimit:   *rowLimit,
		DefaultValueLimit: *valueLimit,
	})

	// ------------------------------------------------------------------
	// Serve over stdio (MCP standard transport).
	// ------------------------------------------------------------------
	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

// env returns the value of the environment variable key, or fallback if unset.
func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

// envInt returns the integer value of an environment variable, or fallback if
// unset or unparseable.
func envInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return fallback
}

func usage() {
	fmt.Fprintf(os.Stderr, `sqlmcp - SQL MCP server

Usage:
  sqlmcp setup            Interactive setup wizard (run this first)
  sqlmcp [flags]          Start the MCP server

Required flags:
  -driver string      SQL driver (env: SQL_DRIVER)
                      Supported: postgres, mysql, sqlite, sqlserver
  -dsn string         Data source name / connection string (env: SQL_DSN)

Optional flags:
  -row-limit int      Default max rows returned by the query tool (env: SQL_ROW_LIMIT, default %d)
  -value-limit int    Default max characters per cell value (env: SQL_VALUE_LIMIT, default %d)
  -help               Show this help

Driver DSN examples:

  postgres:
    postgres://user:password@localhost:5432/mydb?sslmode=disable

  mysql:
    user:password@tcp(localhost:3306)/mydb

  sqlite:
    /path/to/database.db
    file:/path/to/database.db?mode=ro

  sqlserver:
    sqlserver://user:password@localhost:1433?database=mydb

Tools exposed:
  query           - Execute a SELECT (or any row-returning) SQL query
  exec_statement  - Execute INSERT/UPDATE/DELETE/DDL statements
  describe_table  - Show column definitions for a table
  benchmark_query - Measure query execution time over N runs
`, tools.DefaultRowLimit, tools.DefaultValueLimit)
}
