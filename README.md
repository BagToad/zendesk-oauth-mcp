# Zendesk OAuth MCP Server

An MCP server that gives AI agents **read-only** access to Zendesk tickets using your browser session cookie - for use in cases when API tokens are not available. Allow your agents to search tickets, read ticket details, and view conversation history. This server cannot create, update, or delete any Zendesk data.

## Setup

### 1. Download the binary

Download the latest release for your platform from the [releases page](https://github.com/BagToad/zendesk-oauth-mcp/releases).

```bash
# macOS (Apple Silicon)
gh release download --repo BagToad/zendesk-oauth-mcp -p '*darwin_arm64*'

# macOS (Intel)
gh release download --repo BagToad/zendesk-oauth-mcp -p '*darwin_amd64*'

# Linux (x86_64)
gh release download --repo BagToad/zendesk-oauth-mcp -p '*linux_amd64*'
```

```bash
tar -xzf zendesk-oauth-mcp_*.tar.gz
chmod +x zendesk-oauth-mcp

# Move to a directory on your PATH
mv zendesk-oauth-mcp ~/.local/bin/
```

> **Note (macOS):** If macOS blocks the binary, remove the quarantine attribute:
> ```bash
> xattr -dr com.apple.quarantine ~/.local/bin/zendesk-oauth-mcp
> ```

Or build from source:

```bash
gh repo clone BagToad/zendesk-oauth-mcp
cd zendesk-oauth-mcp
go build -o zendesk-oauth-mcp .
```

### 2. Get your Zendesk session cookie

See [Authentication](#authentication) below for how to get your cookie.

### 3. Add to your MCP client

Add the server to your MCP client config. Replace the subdomain and cookie values:

**GitHub Copilot CLI** - add to `~/.copilot/mcp-config.json`:

```json
{
  "mcpServers": {
    "zendesk": {
      "command": "zendesk-oauth-mcp",
      "args": [],
      "env": {
        "ZENDESK_SUBDOMAIN": "your-subdomain",
        "ZENDESK_COOKIE": "your-cookie-string"
      }
    }
  }
}
```

> If the binary isn't on your `PATH`, use the full path to the binary instead (e.g. `/Users/you/.local/bin/zendesk-oauth-mcp`).

### 4. Restart your MCP client

Restart your MCP client (e.g. relaunch Copilot CLI) to pick up the new server. You should now have access to `search_tickets`, `get_ticket`, `get_ticket_comments`, and `list_tickets` tools.

### 5. Test the connection

Verify your setup is working by asking Copilot to query your Zendesk instance:

```
show me all open tickets assigned to $USER
```

If everything is configured correctly you should see a list of your open tickets. If you get an authentication error, double-check your cookie and subdomain values.

### 6. Install the Zendesk skill

Copy the included [skill file](.github/skills/zendesk-mcp/SKILL.md) to your personal skills directory so Copilot knows how to use the Zendesk tools across all your projects:

```bash
mkdir -p ~/.copilot/skills/zendesk-mcp
cp .github/skills/zendesk-mcp/SKILL.md ~/.copilot/skills/zendesk-mcp/
```

> If you don't have a local clone of this repo, you can download the file directly:
> ```bash
> mkdir -p ~/.copilot/skills/zendesk-mcp
> gh api repos/BagToad/zendesk-oauth-mcp/contents/.github/skills/zendesk-mcp/SKILL.md \
>   --jq '.content' | base64 -d > ~/.copilot/skills/zendesk-mcp/SKILL.md
> ```

## Authentication

This server authenticates to Zendesk using your browser's session cookie. This means it has the same permissions as your logged-in Zendesk account with no admin setup required.

Two environment variables are needed:

| Variable | Description | Example |
|---|---|---|
| `ZENDESK_SUBDOMAIN` | Your Zendesk subdomain | `xyz.zendesk.com` |
| `ZENDESK_COOKIE` | FULL `Cookie` header value from an authenticated browser session | `_zendesk_cookie=...;_zendesk_shared_session=...` |

### Getting Your Cookie

The session cookie will expire periodically and need to be refreshed. There are two ways to get it:

#### Option A: Manual (Any Browser)

1. Log in to your Zendesk instance (e.g. `https://xyz.zendesk.com`)
2. Open **Developer Tools** → **Network** tab
3. Click on any API request to `zendesk.com`
4. Find the `Cookie` header in the **Request Headers**
5. Copy the entire cookie string

On step 2, I find it easier to right-click on an API request and select "Copy → Copy Request Headers" to get the full headers, then paste into a text editor and extract the full `Cookie` header. Otherwise you may get frustrated and confused by the browser's cookie viewer which splits cookies into individual name/value pairs and doesn't show the full string needed for the header.

#### Option B: Automated via SQLite (Firefox / Zen Browser)

If you use Firefox or a Firefox-based browser (like Zen), an agent can extract the cookie directly from the browser's SQLite cookie database. This is useful for scripting or when an agent needs to refresh the cookie autonomously.

**How it works:**

Firefox-based browsers store cookies in a SQLite database at a known path. The cookies are stored unencrypted (unlike Chromium-based browsers which encrypt cookies using the system keychain).

**Cookie database locations:**

| Browser | Path |
|---|---|
| Firefox (macOS) | `~/Library/Application Support/Firefox/Profiles/<profile>/cookies.sqlite` |
| Firefox (Linux) | `~/.mozilla/firefox/<profile>/cookies.sqlite` |
| Firefox (Windows) | `%APPDATA%\Mozilla\Firefox\Profiles\<profile>\cookies.sqlite` |
| Zen (macOS) | `~/Library/Application Support/zen/Profiles/<profile>/cookies.sqlite` |

> **Note:** Firefox locks the database while running. Copy the file before querying:
> ```bash
> cp ~/Library/Application\ Support/zen/Profiles/<profile>/cookies.sqlite /tmp/cookies.sqlite
> ```

**SQL query to extract the Zendesk cookies:**

```sql
-- Replace 'your-subdomain' with your Zendesk subdomain (e.g. 'xyz')
SELECT
  name,
  value
FROM moz_cookies
WHERE host LIKE '%your-subdomain.zendesk.com%'
  AND name IN ('_zendesk_cookie', '_zendesk_shared_session', '_zendesk_authenticated')
ORDER BY lastAccessed DESC;
```

**Building the cookie string from the query results:**

Concatenate the results into a single string in `name=value; name=value` format:

```bash
# Example: one-liner to extract and format the cookie
sqlite3 /tmp/cookies.sqlite \
  "SELECT name || '=' || value FROM moz_cookies WHERE host LIKE '%xyz.zendesk.com%' AND name IN ('_zendesk_cookie', '_zendesk_shared_session')" \
  | paste -sd '; ' -
```

This outputs a string like:
```
_zendesk_cookie=BAhJIkt7Im...; _zendesk_shared_session=VU11ODZE...
```

Use this value as your `ZENDESK_COOKIE` environment variable.

> **⚠️ Security Note:** The session cookie grants full access to your Zendesk account. Treat it like a password - don't commit it to source control or share it in logs.

## Tools & Usage

This server provides four MCP tools: `search_tickets`, `get_ticket`, `get_ticket_comments`, and `list_tickets`.

For detailed tool documentation, parameters, search syntax, and agent usage tips, see the [skill file](.github/skills/zendesk-mcp/SKILL.md).

This repo also includes a [Zendesk Ticket Investigator agent](.github/agents/zendesk-investigator.agent.md) that can perform end-to-end ticket investigations - gathering context, searching for related tickets, tracing root causes, and producing structured summaries. If you have the [`gh-slack`](https://github.com/rneatherway/gh-slack) extension installed, the agent can also search Slack for internal discussions related to a ticket.

### Common errors

| Error | Cause | Fix |
|---|---|---|
| `401 Unauthorized` | Cookie has expired | Refresh the `ZENDESK_COOKIE` value |
| `403 Forbidden` | Insufficient permissions | Ensure the authenticated user has agent access |
| `404 Not Found` | Invalid ticket ID | Verify the ticket ID exists |
| `429 Too Many Requests` | Rate limited | Wait and retry; reduce request frequency |

## Development

```bash
go build -o zendesk-oauth-mcp .   # Build the binary
go run .                          # Run without building
```

## License

MIT
