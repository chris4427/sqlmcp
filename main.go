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

const version = "0.1.0"

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
		driverStr = flag.String("driver", env("SQL_DRIVER", ""), "SQL driver: postgres | mysql | sqlite | sqlserver")
		dsn       = flag.String("dsn", env("SQL_DSN", ""), "Data source name / connection string")
		showHelp  = flag.Bool("help", false, "Show usage")
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

	tools.RegisterAll(s, sqlDB, string(driver))

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

func usage() {
	fmt.Fprintf(os.Stderr, `sqlmcp - SQL MCP server

Usage:
  sqlmcp setup            Interactive setup wizard (run this first)
  sqlmcp [flags]          Start the MCP server

Flags:
  -driver string   SQL driver to use (env: SQL_DRIVER)
                   Supported: postgres, mysql, sqlite, sqlserver
  -dsn string      Data source name / connection string (env: SQL_DSN)
  -help            Show this help

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
`)
}
