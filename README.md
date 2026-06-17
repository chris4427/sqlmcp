# sqlmcp

A Model Context Protocol (MCP) server that connects to a SQL database and exposes tools for querying it. Works with any MCP-compatible client: Claude Desktop, opencode, Kiro, Cursor, etc.

## Install

Run this one-liner — no Go required:

```sh
curl -fsSL https://raw.githubusercontent.com/chris4427/sqlmcp/main/install.sh | bash
```

The script will:
1. Download the right binary for your OS and architecture
2. Ask which MCP client you use
3. Ask for your database type and connection string
4. Write the config to the right place automatically

Then restart your MCP client and you're done.

---

## Tools

| Tool | Description |
|---|---|
| `query` | Run a SELECT (or any row-returning) SQL query, returns JSON |
| `exec_statement` | Run INSERT / UPDATE / DELETE / DDL statements |
| `describe_table` | Show column definitions for a table |

## Supported databases

| Value | Aliases | Notes |
|---|---|---|
| `postgres` | `postgresql`, `pg` | Requires a running PostgreSQL server |
| `mysql` | `mariadb` | Requires a running MySQL/MariaDB server |
| `sqlite` | `sqlite3` | File path or `:memory:` — no server needed |
| `sqlserver` | `mssql` | Requires a running SQL Server instance |

---

## Reconfigure

Already have the binary but want to change your client or database? Run the setup wizard:

```sh
sqlmcp setup
```

---

## Manual setup

If you prefer to configure things yourself:

**DSN examples**

| Database | Example |
|---|---|
| PostgreSQL | `postgres://user:password@localhost:5432/mydb?sslmode=disable` |
| MySQL | `user:password@tcp(localhost:3306)/mydb` |
| SQLite | `/path/to/database.db` or `:memory:` |
| SQL Server | `sqlserver://user:password@localhost:1433?database=mydb` |

**Claude Desktop** — edit `~/Library/Application Support/Claude/claude_desktop_config.json`:
```json
{
  "mcpServers": {
    "sqlmcp": {
      "command": "/path/to/sqlmcp",
      "args": ["-driver", "postgres", "-dsn", "YOUR_DSN"]
    }
  }
}
```

**opencode** — edit `opencode.json` in your project:
```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "sqlmcp": {
      "type": "local",
      "command": ["/path/to/sqlmcp", "-driver", "postgres", "-dsn", "YOUR_DSN"],
      "enabled": true
    }
  }
}
```

**Cursor** — edit `~/.cursor/mcp.json`:
```json
{
  "mcpServers": {
    "sqlmcp": {
      "command": "/path/to/sqlmcp",
      "args": ["-driver", "postgres", "-dsn", "YOUR_DSN"]
    }
  }
}
```

**Kiro** — edit `.kiro/settings/mcp.json` in your project:
```json
{
  "mcpServers": {
    "sqlmcp": {
      "command": "/path/to/sqlmcp",
      "args": ["-driver", "postgres", "-dsn", "YOUR_DSN"]
    }
  }
}
```

---

## Quick smoke test (no database needed)

```sh
printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}\n' \
  | sqlmcp -driver sqlite -dsn ':memory:'
```

You should see an `initialize` response with `"name":"sqlmcp"`.

---

## Security note

Your DSN contains credentials. Do not commit it to source control. Use environment variables or a secrets manager rather than hardcoding credentials in config files.
