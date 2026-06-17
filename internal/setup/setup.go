package setup

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Run runs the interactive setup wizard and writes the MCP client config.
func Run(binaryPath string) error {
	s := &scanner{r: bufio.NewReader(os.Stdin)}

	fmt.Println()
	fmt.Println("sqlmcp setup wizard")
	fmt.Println("───────────────────────────────────────")
	fmt.Println()

	// 1. MCP client
	client, err := s.choice("Which MCP client do you use?", []string{
		"Claude Desktop",
		"opencode",
		"Cursor",
		"Kiro",
		"Other (show config snippet)",
	})
	if err != nil {
		return err
	}
	fmt.Println()

	// 2. Database driver
	driver, err := s.choice("Which database are you connecting to?", []string{
		"postgres",
		"mysql",
		"sqlite",
		"sqlserver",
	})
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Printf("  hint: %s\n", dsnHint(driver))

	// 3. DSN
	dsn, err := s.prompt("Connection string (DSN)", "")
	if err != nil {
		return err
	}
	fmt.Println()

	// 4. Write config
	snippet := buildSnippet(client, binaryPath, driver, dsn)

	switch client {
	case "Claude Desktop":
		err = writeFile(claudeDesktopConfigPath(), snippet, "Claude Desktop")
	case "opencode":
		err = writeFile("opencode.json", snippet, "opencode")
	case "Cursor":
		path := filepath.Join(homeDir(), ".cursor", "mcp.json")
		err = writeFile(path, snippet, "Cursor")
	case "Kiro":
		err = writeFile(filepath.Join(".kiro", "settings", "mcp.json"), snippet, "Kiro")
	default:
		fmt.Println("Add this to your MCP client's config:")
		fmt.Println()
		fmt.Println(snippet)
	}
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Done! Restart your MCP client to activate sqlmcp.")
	fmt.Println()
	return nil
}

// ---------------------------------------------------------------------------
// Config builders
// ---------------------------------------------------------------------------

func buildSnippet(client, binaryPath, driver, dsn string) string {
	args, _ := json.Marshal([]string{binaryPath, "-driver", driver, "-dsn", dsn})

	switch client {
	case "opencode":
		return fmt.Sprintf(`{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "sqlmcp": {
      "type": "local",
      "command": %s,
      "enabled": true
    }
  }
}`, string(args))
	default:
		return fmt.Sprintf(`{
  "mcpServers": {
    "sqlmcp": {
      "command": %q,
      "args": ["-driver", %q, "-dsn", %q]
    }
  }
}`, binaryPath, driver, dsn)
	}
}

func claudeDesktopConfigPath() string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(homeDir(), "Library", "Application Support", "Claude", "claude_desktop_config.json")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Claude", "claude_desktop_config.json")
	default:
		return filepath.Join(homeDir(), ".config", "Claude", "claude_desktop_config.json")
	}
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

func dsnHint(driver string) string {
	switch driver {
	case "postgres":
		return "postgres://user:password@localhost:5432/mydb?sslmode=disable"
	case "mysql":
		return "user:password@tcp(localhost:3306)/mydb"
	case "sqlite":
		return "/path/to/database.db  or  :memory:"
	case "sqlserver":
		return "sqlserver://user:password@localhost:1433?database=mydb"
	}
	return ""
}

// ---------------------------------------------------------------------------
// File writer
// ---------------------------------------------------------------------------

func writeFile(path, content, clientName string) error {
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("warning: %s already exists.\n", path)
		fmt.Printf("Add the following to the %q section manually:\n\n", mcpSectionName(clientName))
		fmt.Println(content)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(content+"\n"), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("Config written to %s\n", path)
	return nil
}

func mcpSectionName(client string) string {
	if client == "opencode" {
		return "mcp"
	}
	return "mcpServers"
}

// ---------------------------------------------------------------------------
// Interactive scanner
// ---------------------------------------------------------------------------

type scanner struct {
	r *bufio.Reader
}

func (s *scanner) choice(prompt string, options []string) (string, error) {
	fmt.Printf("%s\n", prompt)
	for i, o := range options {
		fmt.Printf("  %d) %s\n", i+1, o)
	}
	for {
		fmt.Printf("Choice [1-%d]: ", len(options))
		line, err := s.r.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		n := 0
		fmt.Sscanf(line, "%d", &n)
		if n >= 1 && n <= len(options) {
			return options[n-1], nil
		}
		fmt.Printf("  Please enter a number between 1 and %d.\n", len(options))
	}
}

func (s *scanner) prompt(label, defaultVal string) (string, error) {
	for {
		if defaultVal != "" {
			fmt.Printf("%s [%s]: ", label, defaultVal)
		} else {
			fmt.Printf("%s: ", label)
		}
		line, err := s.r.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			line = defaultVal
		}
		if line != "" {
			return line, nil
		}
		fmt.Println("  This field is required.")
	}
}
