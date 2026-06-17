// Package db manages SQL database connections for multiple driver types.
package db

import (
	"database/sql"
	"fmt"
	"strings"

	// Register supported drivers via blank imports.
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/microsoft/go-mssqldb"
	_ "modernc.org/sqlite"
)

// Driver represents a supported SQL driver.
type Driver string

const (
	DriverPostgres   Driver = "postgres"
	DriverMySQL      Driver = "mysql"
	DriverSQLite     Driver = "sqlite"
	DriverSQLServer  Driver = "sqlserver"
)

// ParseDriver maps a user-supplied string to a Driver constant.
// Returns an error if the driver name is not recognised.
func ParseDriver(s string) (Driver, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "postgres", "postgresql", "pg":
		return DriverPostgres, nil
	case "mysql", "mariadb":
		return DriverMySQL, nil
	case "sqlite", "sqlite3":
		return DriverSQLite, nil
	case "sqlserver", "mssql":
		return DriverSQLServer, nil
	default:
		return "", fmt.Errorf(
			"unsupported driver %q; valid values: postgres, mysql, sqlite, sqlserver",
			s,
		)
	}
}

// Open opens a database connection and verifies it with a ping.
func Open(driver Driver, dsn string) (*sql.DB, error) {
	driverName := string(driver)
	if driver == DriverSQLite {
		driverName = "sqlite" // modernc.org/sqlite registers as "sqlite"
	}

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}
