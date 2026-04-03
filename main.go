package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	s := server.NewMCPServer("zendesk", "0.0.1")

	// Initialize cookie from env or browser
	if err := initCookie(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}

	s.AddTool(
		mcp.NewTool("search_tickets",
			mcp.WithDescription("Search Zendesk tickets using Zendesk search syntax. Supports queries like 'status:open', 'priority:high', 'assignee:me', free text, tags, etc."),
			mcp.WithString("query", mcp.Required(), mcp.Description("Zendesk search query (e.g. 'status:open billing issue')")),
			mcp.WithNumber("page", mcp.Description("Page number for pagination")),
			mcp.WithNumber("per_page", mcp.Description("Results per page (max 100)")),
		),
		handleSearchTickets,
	)

	s.AddTool(
		mcp.NewTool("get_ticket",
			mcp.WithDescription("Get full details of a specific Zendesk ticket by ID"),
			mcp.WithNumber("ticket_id", mcp.Required(), mcp.Description("The Zendesk ticket ID")),
		),
		handleGetTicket,
	)

	s.AddTool(
		mcp.NewTool("get_ticket_comments",
			mcp.WithDescription("Get the conversation thread (comments) on a Zendesk ticket"),
			mcp.WithNumber("ticket_id", mcp.Required(), mcp.Description("The Zendesk ticket ID")),
			mcp.WithNumber("page", mcp.Description("Page number for pagination")),
			mcp.WithNumber("per_page", mcp.Description("Results per page (max 100)")),
		),
		handleGetTicketComments,
	)

	s.AddTool(
		mcp.NewTool("list_tickets",
			mcp.WithDescription("List recent Zendesk tickets, optionally filtered by status"),
			mcp.WithString("status", mcp.Enum("new", "open", "pending", "hold", "solved", "closed"), mcp.Description("Filter tickets by status")),
			mcp.WithNumber("page", mcp.Description("Page number for pagination")),
			mcp.WithNumber("per_page", mcp.Description("Results per page (max 100)")),
		),
		handleListTickets,
	)

	s.AddTool(
		mcp.NewTool("export_ticket_markdown",
			mcp.WithDescription("Export a Zendesk ticket as a complete Markdown document with YAML frontmatter. Resolves author names, filters bot comments, downloads attachments as base64, and rewrites attachment URLs to relative paths (./attachments/filename). Returns the markdown string and attachment data."),
			mcp.WithNumber("ticket_id", mcp.Required(), mcp.Description("The Zendesk ticket ID to export")),
			mcp.WithBoolean("include_internal_notes", mcp.Description("Include internal/private comments in the export (default: true)")),
		),
		handleExportTicketMarkdown,
	)

	s.AddTool(
		mcp.NewTool("get_ticket_updates",
			mcp.WithDescription("Get new comments on a ticket since a given timestamp, formatted as Markdown blocks ready to append to an existing export. Returns only new comments with their attachments. Also returns current ticket status so the caller can update frontmatter if needed."),
			mcp.WithNumber("ticket_id", mcp.Required(), mcp.Description("The Zendesk ticket ID")),
			mcp.WithString("since", mcp.Required(), mcp.Description("ISO 8601 timestamp (e.g. 2024-01-15T10:30:00Z). Only comments created after this time are returned.")),
			mcp.WithBoolean("include_internal_notes", mcp.Description("Include internal/private comments (default: true)")),
		),
		handleGetTicketUpdates,
	)

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "Fatal error: %v\n", err)
		os.Exit(1)
	}
}

func textResult(v any) *mcp.CallToolResult {
	b, _ := json.MarshalIndent(v, "", "  ")
	return mcp.NewToolResultText(string(b))
}

func errorResult(msg string, err error) *mcp.CallToolResult {
	return mcp.NewToolResultError(fmt.Sprintf("%s: %v", msg, err))
}

func toSummaries(tickets []ZendeskTicket) []ticketSummary {
	summary := make([]ticketSummary, len(tickets))
	for i, t := range tickets {
		summary[i] = ticketSummary{
			ID:        t.ID,
			Subject:   t.Subject,
			Status:    t.Status,
			Priority:  t.Priority,
			UpdatedAt: t.UpdatedAt,
			Tags:      t.Tags,
		}
	}
	return summary
}

func handleSearchTickets(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query := req.GetString("query", "")
	page := req.GetInt("page", 1)
	perPage := req.GetInt("per_page", 25)

	result, err := searchTickets(query, page, perPage)
	if err != nil {
		return errorResult("Error searching tickets", err), nil
	}

	return textResult(map[string]any{
		"total_count": result.Count,
		"page":        page,
		"tickets":     toSummaries(result.Tickets),
	}), nil
}

func handleGetTicket(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ticketID := req.GetInt("ticket_id", 0)

	result, err := getTicket(ticketID)
	if err != nil {
		return errorResult("Error getting ticket", err), nil
	}

	return textResult(result.Ticket), nil
}

func handleGetTicketComments(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ticketID := req.GetInt("ticket_id", 0)
	page := req.GetInt("page", 1)
	perPage := req.GetInt("per_page", 25)

	result, err := getTicketComments(ticketID, page, perPage)
	if err != nil {
		return errorResult("Error getting comments", err), nil
	}

	comments := make([]commentSummary, len(result.Comments))
	for i, c := range result.Comments {
		attachments := make([]attachmentSummary, len(c.Attachments))
		for j, a := range c.Attachments {
			attachments[j] = attachmentSummary{
				FileName:   a.FileName,
				ContentURL: a.ContentURL,
				Size:       a.Size,
			}
		}
		comments[i] = commentSummary{
			ID:          c.ID,
			AuthorID:    c.AuthorID,
			Body:        c.Body,
			Public:      c.Public,
			CreatedAt:   c.CreatedAt,
			Attachments: attachments,
		}
	}

	return textResult(map[string]any{
		"ticket_id":   ticketID,
		"total_count": result.Count,
		"page":        page,
		"comments":    comments,
	}), nil
}

func handleListTickets(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	status := req.GetString("status", "")
	page := req.GetInt("page", 1)
	perPage := req.GetInt("per_page", 25)

	result, err := listTickets(status, page, perPage)
	if err != nil {
		return errorResult("Error listing tickets", err), nil
	}

	return textResult(map[string]any{
		"total_count": result.Count,
		"page":        page,
		"tickets":     toSummaries(result.Tickets),
	}), nil
}

func handleExportTicketMarkdown(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ticketID := req.GetInt("ticket_id", 0)
	includeInternal := getBool(req, "include_internal_notes", true)

	result, err := exportTicketMarkdown(ticketID, includeInternal)
	if err != nil {
		return errorResult("Error exporting ticket", err), nil
	}

	return textResult(result), nil
}

func handleGetTicketUpdates(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ticketID := req.GetInt("ticket_id", 0)
	since := req.GetString("since", "")
	includeInternal := getBool(req, "include_internal_notes", true)

	if since == "" {
		return errorResult("Missing required parameter", fmt.Errorf("'since' timestamp is required")), nil
	}

	result, err := getTicketUpdatesSince(ticketID, since, includeInternal)
	if err != nil {
		return errorResult("Error getting ticket updates", err), nil
	}

	return textResult(result), nil
}

// getBool extracts a boolean parameter from the request arguments.
func getBool(req mcp.CallToolRequest, name string, defaultVal bool) bool {
	args := req.GetArguments()
	if v, ok := args[name]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultVal
}

// Response shapes for JSON serialization
type ticketSummary struct {
	ID        int      `json:"id"`
	Subject   string   `json:"subject"`
	Status    string   `json:"status"`
	Priority  *string  `json:"priority"`
	UpdatedAt string   `json:"updated_at"`
	Tags      []string `json:"tags"`
}

type commentSummary struct {
	ID          int                 `json:"id"`
	AuthorID    int                 `json:"author_id"`
	Body        string              `json:"body"`
	Public      bool                `json:"public"`
	CreatedAt   string              `json:"created_at"`
	Attachments []attachmentSummary `json:"attachments"`
}

type attachmentSummary struct {
	FileName   string `json:"file_name"`
	ContentURL string `json:"content_url"`
	Size       int    `json:"size"`
}
