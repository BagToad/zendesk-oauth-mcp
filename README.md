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

### 2. Ensure `~/.local/bin` is on your PATH

If `~/.local/bin` isn't already on your `PATH`, add it to your shell profile:

`bash`/`zsh`:
```bash
PATH="$HOME/.local/bin:$PATH"
```
Then restart your shell or run `source ~/.bashrc` / `source ~/.zshrc`.

`fish`:

```fish
fish_add_path ~/.local/bin
```

### 3. Add to your MCP client

Add the server to your MCP client config. The only required setting is `ZENDESK_SUBDOMAIN`:

**GitHub Copilot CLI** - add to `~/.copilot/mcp-config.json`:

```json
{
  "mcpServers": {
    "zendesk": {
      "command": "zendesk-oauth-mcp",
      "args": [],
      "env": {
        "ZENDESK_SUBDOMAIN": "your-subdomain"
      }
    }
  }
}
```

The server will automatically extract your Zendesk session cookie from your browser (see [Authentication](#authentication)). You can also provide the cookie manually by adding `"ZENDESK_COOKIE": "your-cookie-string"` to the `env` block.

> If the binary isn't on your `PATH`, use the full path to the binary instead (e.g. `/Users/you/.local/bin/zendesk-oauth-mcp`).

### 4. Install the Zendesk skill

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

### 5. Install the Zendesk Investigator agent

Copy the included [agent file](.github/agents/zendesk-investigator.agent.md) to your personal agents directory so Copilot can perform end-to-end ticket investigations:

```bash
mkdir -p ~/.copilot/agents
cp .github/agents/zendesk-investigator.agent.md ~/.copilot/agents/
```

> If you don't have a local clone of this repo, you can download the file directly:
> ```bash
> mkdir -p ~/.copilot/agents
> gh api repos/BagToad/zendesk-oauth-mcp/contents/.github/agents/zendesk-investigator.agent.md \
>   --jq '.content' | base64 -d > ~/.copilot/agents/zendesk-investigator.agent.md
> ```

### 6. Restart your MCP client

Restart your MCP client (e.g. relaunch Copilot CLI) to pick up the new server, skill, and agent. You should now have access to `search_tickets`, `get_ticket`, `get_ticket_comments`, and `list_tickets` tools.

### 7. Test the connection

Verify your setup is working by asking Copilot to query your Zendesk instance:

```
show me all open tickets assigned to $USER
```

If everything is configured correctly you should see a list of your open tickets. If you get an authentication error, double-check your cookie and subdomain values.

## Updates

To update to the latest version, download the new binary and replace the existing one:

```bash
# macOS (Apple Silicon)
gh release download --repo BagToad/zendesk-oauth-mcp -p '*darwin_arm64*' --clobber

# macOS (Intel)
gh release download --repo BagToad/zendesk-oauth-mcp -p '*darwin_amd64*' --clobber

# Linux (x86_64)
gh release download --repo BagToad/zendesk-oauth-mcp -p '*linux_amd64*' --clobber
```

```bash
tar -xzf zendesk-oauth-mcp_*.tar.gz
chmod +x zendesk-oauth-mcp
mv -f zendesk-oauth-mcp ~/.local/bin/
```

> **Note (macOS):** If macOS blocks the updated binary, remove the quarantine attribute:
> ```bash
> xattr -dr com.apple.quarantine ~/.local/bin/zendesk-oauth-mcp
> ```

Then restart your MCP client to pick up the new version.

## Authentication

This server authenticates to Zendesk using your browser's session cookie. This means it has the same permissions as your logged-in Zendesk account with no admin setup required.

### Automatic Cookie Extraction (Recommended)

When `ZENDESK_COOKIE` is not set, the server automatically extracts cookies from your browser's cookie database on startup using [kooky](https://github.com/browserutils/kooky). Supported browsers:

| Browser | macOS | Linux |
|---|---|---|
| Zen | ✅ | ✅ |
| Firefox | ✅ | ✅ |
| Safari | ✅ | - |
| Chrome | ✅ | ✅ |
| Edge | ✅ | ✅ |

The server searches browsers in the order listed above. Once it finds valid Zendesk cookies, it stops searching. This means **Zen and Firefox are checked first** and don't require any password prompts.

If the cookie expires mid-session (401 error), the server will automatically re-extract from the browser and retry the request.

> **Chrome on macOS:** Chrome encrypts cookies using the macOS Keychain. The first time the server reads Chrome cookies, macOS will show a password prompt to allow Keychain access. You can grant "Always Allow" in Keychain Access, but this resets whenever the binary is rebuilt. To avoid the prompt entirely, ensure you're logged into Zendesk in a non-Chromium browser (Zen, Firefox, or Safari).

### Environment Variables

| Variable | Required | Description |
|---|---|---|
| `ZENDESK_SUBDOMAIN` | Yes | Your Zendesk subdomain (e.g. `mycompany` for `mycompany.zendesk.com`) |
| `ZENDESK_COOKIE` | No | Full `Cookie` header value. If omitted, the server extracts cookies from your browser automatically. |

### Manual Cookie Setup

If automatic extraction doesn't work for your setup, you can provide the cookie manually:

1. Log in to your Zendesk instance in your browser
2. Open **Developer Tools** → **Network** tab
3. Click on any request to `zendesk.com`
4. Find the `Cookie` header in the **Request Headers**
5. Copy the entire cookie string and set it as `ZENDESK_COOKIE` in your MCP config

> **⚠️ Security Note:** The session cookie grants full access to your Zendesk account. Treat it like a password - don't commit it to source control or share it in logs.

## Tools & Usage

This server provides four MCP tools: `search_tickets`, `get_ticket`, `get_ticket_comments`, and `list_tickets`.

For detailed tool documentation, parameters, search syntax, and agent usage tips, see the [skill file](.github/skills/zendesk-mcp/SKILL.md).

This repo also includes a [Zendesk Ticket Investigator agent](.github/agents/zendesk-investigator.agent.md) that can perform end-to-end ticket investigations - gathering context, searching for related tickets, tracing root causes, and producing structured summaries. If you have the [`gh-slack`](https://github.com/rneatherway/gh-slack) extension installed, the agent can also search Slack for internal discussions related to a ticket.

### Common errors

| Error | Cause | Fix |
|---|---|---|
| `spawn zendesk-oauth-mcp ENOENT` | Binary not found on your `PATH` | Ensure `~/.local/bin` is on your `PATH` (see step 2) and that the binary is located there. |
| `spawn zendesk-oauth-mcp EACCES` | Binary missing execute permission | Run `chmod +x ~/.local/bin/zendesk-oauth-mcp` |
| `401 Unauthorized` | Cookie has expired | The server auto-retries by re-extracting from your browser. If this persists, log into Zendesk in your browser to refresh the session. |
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
