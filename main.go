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
