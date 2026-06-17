#!/usr/bin/env bash
set -euo pipefail

# ---------------------------------------------------------------------------
# sqlmcp installer
# Usage: curl -fsSL https://raw.githubusercontent.com/yourname/sqlmcp/main/install.sh | bash
# ---------------------------------------------------------------------------

REPO="chris4427/sqlmcp"
BINARY_NAME="sqlmcp"
INSTALL_DIR="${HOME}/.local/bin"

# ---- colours ---------------------------------------------------------------
if [ -t 1 ]; then
  BOLD="\033[1m"
  GREEN="\033[32m"
  CYAN="\033[36m"
  YELLOW="\033[33m"
  RED="\033[31m"
  RESET="\033[0m"
else
  BOLD="" GREEN="" CYAN="" YELLOW="" RED="" RESET=""
fi

info()    { echo -e "${CYAN}${BOLD}==>${RESET} $*"; }
success() { echo -e "${GREEN}${BOLD}✓${RESET} $*"; }
warn()    { echo -e "${YELLOW}${BOLD}!${RESET} $*"; }
die()     { echo -e "${RED}${BOLD}error:${RESET} $*" >&2; exit 1; }

# ---- prompt helper ---------------------------------------------------------
# ask <variable_name> <prompt> [default]
ask() {
  local var="$1" prompt="$2" default="${3:-}"
  local display_prompt
  if [ -n "$default" ]; then
    display_prompt="${BOLD}${prompt}${RESET} [${default}]: "
  else
    display_prompt="${BOLD}${prompt}${RESET}: "
  fi
  while true; do
    echo -en "$display_prompt"
    read -r input
    input="${input:-$default}"
    if [ -n "$input" ]; then
      eval "$var=\"\$input\""
      return
    fi
    warn "This field is required."
  done
}

# ask_choice <variable_name> <prompt> <option1> <option2> ...
ask_choice() {
  local var="$1" prompt="$2"
  shift 2
  local options=("$@")
  echo -e "${BOLD}${prompt}${RESET}"
  for i in "${!options[@]}"; do
    echo "  $((i+1))) ${options[$i]}"
  done
  while true; do
    echo -en "${BOLD}Choice [1-${#options[@]}]${RESET}: "
    read -r choice
    if [[ "$choice" =~ ^[0-9]+$ ]] && [ "$choice" -ge 1 ] && [ "$choice" -le "${#options[@]}" ]; then
      eval "$var=\"\${options[$((choice-1))]}\""
      return
    fi
    warn "Please enter a number between 1 and ${#options[@]}."
  done
}

# ---- detect OS / arch ------------------------------------------------------
detect_platform() {
  local os arch

  case "$(uname -s)" in
    Linux)  os="linux" ;;
    Darwin) os="darwin" ;;
    MINGW*|MSYS*|CYGWIN*) os="windows" ;;
    *) die "Unsupported OS: $(uname -s)" ;;
  esac

  case "$(uname -m)" in
    x86_64|amd64)  arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) die "Unsupported architecture: $(uname -m)" ;;
  esac

  echo "${os}_${arch}"
}

# ---- download binary -------------------------------------------------------
download_binary() {
  local platform="$1"
  local ext=""
  [[ "$platform" == windows_* ]] && ext=".exe"

  local filename="${BINARY_NAME}_${platform}${ext}"
  local url="https://github.com/${REPO}/releases/latest/download/${filename}"
  local dest="${INSTALL_DIR}/${BINARY_NAME}${ext}"

  info "Downloading ${BINARY_NAME} for ${platform}..."

  mkdir -p "$INSTALL_DIR"

  if command -v curl &>/dev/null; then
    curl -fsSL "$url" -o "$dest"
  elif command -v wget &>/dev/null; then
    wget -qO "$dest" "$url"
  else
    die "Neither curl nor wget found. Please install one and retry."
  fi

  chmod +x "$dest"
  echo "$dest"
}

# ---- config writers --------------------------------------------------------

write_claude_desktop() {
  local binary_path="$1" driver="$2" dsn="$3"
  local config_file

  case "$(uname -s)" in
    Darwin)  config_file="${HOME}/Library/Application Support/Claude/claude_desktop_config.json" ;;
    Linux)   config_file="${HOME}/.config/Claude/claude_desktop_config.json" ;;
    MINGW*|MSYS*|CYGWIN*) config_file="${APPDATA}/Claude/claude_desktop_config.json" ;;
  esac

  mkdir -p "$(dirname "$config_file")"

  local snippet
  snippet=$(printf '{
  "mcpServers": {
    "sqlmcp": {
      "command": "%s",
      "args": ["-driver", "%s", "-dsn", "%s"]
    }
  }
}' "$binary_path" "$driver" "$dsn")

  if [ -f "$config_file" ]; then
    warn "Claude Desktop config already exists at:"
    warn "  ${config_file}"
    warn "Add the following to the \"mcpServers\" section manually:"
    echo ""
    echo "$snippet"
  else
    echo "$snippet" > "$config_file"
    success "Claude Desktop config written to ${config_file}"
  fi
}

write_opencode() {
  local binary_path="$1" driver="$2" dsn="$3"
  local config_file="./opencode.json"

  local snippet
  snippet=$(printf '{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "sqlmcp": {
      "type": "local",
      "command": ["%s", "-driver", "%s", "-dsn", "%s"],
      "enabled": true
    }
  }
}' "$binary_path" "$driver" "$dsn")

  if [ -f "$config_file" ]; then
    warn "opencode.json already exists in the current directory."
    warn "Add the following to the \"mcp\" section manually:"
    echo ""
    echo "$snippet"
  else
    echo "$snippet" > "$config_file"
    success "opencode config written to ${config_file}"
  fi
}

write_cursor() {
  local binary_path="$1" driver="$2" dsn="$3"
  local config_file="${HOME}/.cursor/mcp.json"

  mkdir -p "$(dirname "$config_file")"

  local snippet
  snippet=$(printf '{
  "mcpServers": {
    "sqlmcp": {
      "command": "%s",
      "args": ["-driver", "%s", "-dsn", "%s"]
    }
  }
}' "$binary_path" "$driver" "$dsn")

  if [ -f "$config_file" ]; then
    warn "Cursor config already exists at:"
    warn "  ${config_file}"
    warn "Add the following to the \"mcpServers\" section manually:"
    echo ""
    echo "$snippet"
  else
    echo "$snippet" > "$config_file"
    success "Cursor config written to ${config_file}"
  fi
}

write_kiro() {
  local binary_path="$1" driver="$2" dsn="$3"
  local config_file="./.kiro/settings/mcp.json"

  mkdir -p "$(dirname "$config_file")"

  local snippet
  snippet=$(printf '{
  "mcpServers": {
    "sqlmcp": {
      "command": "%s",
      "args": ["-driver", "%s", "-dsn", "%s"]
    }
  }
}' "$binary_path" "$driver" "$dsn")

  if [ -f "$config_file" ]; then
    warn "Kiro config already exists at:"
    warn "  ${config_file}"
    warn "Add the following to the \"mcpServers\" section manually:"
    echo ""
    echo "$snippet"
  else
    echo "$snippet" > "$config_file"
    success "Kiro config written to ${config_file}"
  fi
}

# ---- DSN helper ------------------------------------------------------------

dsn_hint() {
  local driver="$1"
  case "$driver" in
    postgres)   echo "e.g. postgres://user:password@localhost:5432/mydb?sslmode=disable" ;;
    mysql)      echo "e.g. user:password@tcp(localhost:3306)/mydb" ;;
    sqlite)     echo "e.g. /path/to/database.db  or  :memory:" ;;
    sqlserver)  echo "e.g. sqlserver://user:password@localhost:1433?database=mydb" ;;
  esac
}

# ---- main ------------------------------------------------------------------

main() {
  echo ""
  echo -e "${BOLD}sqlmcp installer${RESET}"
  echo "─────────────────────────────────────────"
  echo ""

  # 1. Detect platform and download
  local platform
  platform=$(detect_platform)

  local binary_path
  binary_path=$(download_binary "$platform")
  success "Binary installed to ${binary_path}"

  # Ensure install dir is on PATH
  if [[ ":$PATH:" != *":${INSTALL_DIR}:"* ]]; then
    warn "${INSTALL_DIR} is not on your PATH."
    warn "Add this to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
    warn "  export PATH=\"\$PATH:${INSTALL_DIR}\""
  fi

  echo ""

  # 2. Choose MCP client
  local client
  ask_choice client "Which MCP client do you use?" \
    "Claude Desktop" \
    "opencode" \
    "Cursor" \
    "Kiro" \
    "Other (show me the config snippet)"

  echo ""

  # 3. Choose database driver
  local driver
  ask_choice driver "Which database are you connecting to?" \
    "postgres" \
    "mysql" \
    "sqlite" \
    "sqlserver"

  echo ""
  echo -e "  ${YELLOW}$(dsn_hint "$driver")${RESET}"

  # 4. Get DSN
  local dsn
  ask dsn "Connection string (DSN)"

  echo ""

  # 5. Write config
  case "$client" in
    "Claude Desktop") write_claude_desktop "$binary_path" "$driver" "$dsn" ;;
    "opencode")       write_opencode       "$binary_path" "$driver" "$dsn" ;;
    "Cursor")         write_cursor         "$binary_path" "$driver" "$dsn" ;;
    "Kiro")           write_kiro           "$binary_path" "$driver" "$dsn" ;;
    "Other"*)
      info "Add this to your MCP client's config:"
      printf '{
  "mcpServers": {
    "sqlmcp": {
      "command": "%s",
      "args": ["-driver", "%s", "-dsn", "%s"]
    }
  }
}\n' "$binary_path" "$driver" "$dsn"
      ;;
  esac

  echo ""
  success "Done! Restart your MCP client to activate sqlmcp."
  echo ""
}

main "$@"
