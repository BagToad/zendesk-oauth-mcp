---
name: zendesk-mcp
description: >
  Searches and reads Zendesk tickets via MCP tools. Provides search, ticket
  details, conversation threads, and ticket listing with full Zendesk search
  syntax support. Authenticates using a browser session cookie.
---

# Zendesk MCP

You have access to a Zendesk instance through four MCP tools. Use them to
search tickets, read ticket details, and review conversation threads.

## Available Tools

### `search_tickets`

Search for tickets using [Zendesk search syntax](https://support.zendesk.com/hc/en-us/articles/4408886879258).

**Parameters:**
- `query` (required) — Zendesk search query string
- `page` (optional, default: 1) — page number
- `per_page` (optional, default: 25, max: 100) — results per page

**Search syntax quick reference:**
- `status:open` / `status:pending` / `status:solved` — filter by status
- `priority:high` / `priority:urgent` — filter by priority
- `assignee:user@example.com` — filter by assigned agent
- `requester:customer@company.com` — filter by ticket requester
- `organization:acme-corp` — filter by organization name
- `tags:tag_name` — filter by tag
- `created>2026-01-01` / `updated<2026-03-01` — date filters
- `"exact phrase"` — exact match in subject/description
- Free text — searched across subjects and descriptions
- Combine any of the above: `status:open priority:high tags:billing assignee:me`

Results are sorted by most recently updated first.

### `get_ticket`

Fetch full details of a single ticket by numeric ID.

**Parameters:**
- `ticket_id` (required) — the Zendesk ticket ID number

**Returns:** Complete ticket object including subject, description, status,
priority, type, tags, custom fields, requester/assignee/group IDs, timestamps,
and organization info.

### `get_ticket_comments`

Fetch the full conversation thread on a ticket. Includes both public customer
replies and internal agent notes.

**Parameters:**
- `ticket_id` (required) — the Zendesk ticket ID number
- `page` (optional, default: 1) — page number
- `per_page` (optional, default: 25, max: 100) — results per page

**Each comment includes:**
- `body` — the comment text (may contain markdown, HTML, or plain text)
- `public` — `true` for customer-visible replies, `false` for internal notes
- `author_id` — numeric Zendesk user ID of the author
- `created_at` — ISO 8601 timestamp
- `attachments` — array of file attachments with name, URL, and size

**Pagination:** Check `total_count` in the response. If it exceeds `per_page`,
fetch subsequent pages with `page: 2`, `page: 3`, etc.

### `list_tickets`

List recent tickets, optionally filtered by status.

**Parameters:**
- `status` (optional) — one of: `new`, `open`, `pending`, `hold`, `solved`, `closed`
- `page` (optional, default: 1) — page number
- `per_page` (optional, default: 25, max: 100) — results per page

Results are sorted by most recently updated first.

## Search Examples

```
search_tickets("requester:customer@company.com")
search_tickets("organization:acme-corp status:open")
search_tickets("assignee:agent@company.com status:open status:pending")
search_tickets("login failed rate limit tags:authentication")
search_tickets("tags:billing status:open")
```

## Tips

- **Tags are powerful.** Zendesk tickets are typically auto-tagged by product
  area. Use tag-based searches when free text is too noisy.
- **Combine filters freely.** All search operators can be combined in a single
  query string.
- **Large tickets.** Some tickets have 50-100+ comments. Always check
  `total_count` and paginate if needed.
- **Internal vs public.** Comments with `public: false` are internal agent notes
  — these often contain the most useful investigation context.
