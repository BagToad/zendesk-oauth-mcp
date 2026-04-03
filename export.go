package main

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
)

var nonAlphanumDot = regexp.MustCompile(`[^a-z0-9.]+`)
var reEmptyAnchor = regexp.MustCompile(`\[]\(#[^)]*\)`)

// sanitizeFilename lowercases, replaces spaces/special chars with hyphens,
// and collapses runs of hyphens. "Image (3).PNG" → "image-3.png"
func sanitizeFilename(name string) string {
	ext := ""
	if i := strings.LastIndex(name, "."); i >= 0 {
		ext = strings.ToLower(name[i:])
		name = name[:i]
	}
	name = strings.ToLower(name)
	name = nonAlphanumDot.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	return name + ext
}

// ExportAttachment holds a downloaded attachment as base64 data.
type ExportAttachment struct {
	Filename string `json:"filename"`
	Data     string `json:"data"`
	Size     int    `json:"size"`
}

// isBotComment returns true if the comment should be filtered out as bot-generated.
// Uses the ZENDESK_BOT_IDS env var (comma-separated author IDs) to identify bots.
func isBotComment(comment ZendeskComment) bool {
	botIDs := os.Getenv("ZENDESK_BOT_IDS")
	if botIDs == "" {
		return false
	}
	for _, idStr := range strings.Split(botIDs, ",") {
		if id, err := strconv.Atoi(strings.TrimSpace(idStr)); err == nil && id == comment.AuthorID {
			return true
		}
	}
	return false
}

func formatTimestamp(iso string) string {
	if t, err := time.Parse(time.RFC3339, iso); err == nil {
		return t.UTC().Format("2006-01-02 15:04 UTC")
	}
	return iso
}

func getUserDisplayName(authorID int, users map[int]ZendeskUser) string {
	if u, ok := users[authorID]; ok {
		return u.Name
	}
	return fmt.Sprintf("User %d", authorID)
}

func renderFrontmatter(ticket ZendeskTicket, users map[int]ZendeskUser, orgName string) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("ticket_id: %d\n", ticket.ID))
	sb.WriteString(fmt.Sprintf("subject: %q\n", ticket.Subject))
	sb.WriteString(fmt.Sprintf("requester: %s\n", getUserDisplayName(ticket.RequesterID, users)))
	if ticket.AssigneeID != nil {
		sb.WriteString(fmt.Sprintf("assignee: %s\n", getUserDisplayName(*ticket.AssigneeID, users)))
	}
	if orgName != "" {
		sb.WriteString(fmt.Sprintf("organization: %s\n", orgName))
	}
	sb.WriteString(fmt.Sprintf("created_at: %s\n", ticket.CreatedAt))
	if ticket.Priority != nil {
		sb.WriteString(fmt.Sprintf("priority: %s\n", *ticket.Priority))
	}
	sb.WriteString(fmt.Sprintf("status: %s\n", ticket.Status))
	if ticket.Type != nil {
		sb.WriteString(fmt.Sprintf("type: %s\n", *ticket.Type))
	}
	sb.WriteString("---\n")
	return sb.String()
}

func renderComment(comment ZendeskComment, users map[int]ZendeskUser) string {
	var sb strings.Builder
	name := getUserDisplayName(comment.AuthorID, users)
	ts := formatTimestamp(comment.CreatedAt)

	if comment.Public {
		sb.WriteString(fmt.Sprintf("### %s — %s\n\n", name, ts))
	} else {
		sb.WriteString(fmt.Sprintf("### 🔒 %s — %s (internal)\n\n", name, ts))
	}

	// Use html_body when available — it preserves tables and formatting.
	// Fall back to plain body if html_body is empty.
	body := comment.Body
	if comment.HTMLBody != "" {
		body = htmlBodyToMarkdown(comment.HTMLBody)
	}

	// Strip empty anchor links from headings (e.g. "[](#anchor-id)")
	// These are artifacts from Zendesk's HTML heading markup.
	body = reEmptyAnchor.ReplaceAllString(body, "")

	for _, a := range comment.Attachments {
		if a.ContentURL != "" && a.FileName != "" {
			sanitized := sanitizeFilename(a.FileName)
			replacement := fmt.Sprintf("./attachments/%s", sanitized)
			// Replace both the raw URL and any URL-encoded variant
			body = strings.ReplaceAll(body, a.ContentURL, replacement)
			if encoded := url.PathEscape(a.FileName); encoded != a.FileName {
				body = strings.ReplaceAll(body, encoded, sanitized)
			}
			// Also try matching the URL by its unique token path
			if token := extractZendeskToken(a.ContentURL); token != "" {
				body = replaceURLContaining(body, token, replacement)
			}
		}
	}

	sb.WriteString(body)
	sb.WriteString("\n")
	return sb.String()
}

// processAttachments downloads attachments from filtered comments and returns them as base64.
func processAttachments(comments []ZendeskComment) []ExportAttachment {
	var attachments []ExportAttachment
	for _, c := range comments {
		for _, a := range c.Attachments {
			data, err := downloadAttachment(a.ContentURL)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not download attachment %s: %v\n", a.FileName, err)
				continue
			}
			attachments = append(attachments, ExportAttachment{
				Filename: sanitizeFilename(a.FileName),
				Data:     base64.StdEncoding.EncodeToString(data),
				Size:     len(data),
			})
		}
	}
	return attachments
}

// filterComments removes bot comments and optionally internal notes.
func filterComments(comments []ZendeskComment, includeInternal bool) []ZendeskComment {
	var filtered []ZendeskComment
	for _, c := range comments {
		if isBotComment(c) {
			continue
		}
		if !includeInternal && !c.Public {
			continue
		}
		filtered = append(filtered, c)
	}
	return filtered
}

// exportTicketMarkdown builds a full markdown export of a ticket with all comments.
func exportTicketMarkdown(ticketID int, includeInternal bool) (map[string]any, error) {
	ticketRes, err := getTicket(ticketID)
	if err != nil {
		return nil, fmt.Errorf("fetching ticket: %w", err)
	}
	ticket := ticketRes.Ticket

	comments, err := getAllTicketComments(ticketID)
	if err != nil {
		return nil, fmt.Errorf("fetching comments: %w", err)
	}

	// Collect all author IDs for batch resolution
	authorIDs := []int{ticket.RequesterID}
	if ticket.AssigneeID != nil {
		authorIDs = append(authorIDs, *ticket.AssigneeID)
	}
	for _, c := range comments {
		authorIDs = append(authorIDs, c.AuthorID)
	}

	users, err := getUsers(authorIDs)
	if err != nil {
		return nil, fmt.Errorf("resolving users: %w", err)
	}

	var orgName string
	if ticket.OrganizationID != nil {
		if org, err := getOrganization(*ticket.OrganizationID); err == nil {
			orgName = org.Name
		}
	}

	filtered := filterComments(comments, includeInternal)
	attachments := processAttachments(filtered)

	var md strings.Builder
	md.WriteString(renderFrontmatter(ticket, users, orgName))
	md.WriteString(fmt.Sprintf("\n# %s\n\n## Conversation\n\n", ticket.Subject))

	for i, c := range filtered {
		if i > 0 {
			md.WriteString("\n---\n\n")
		}
		md.WriteString(renderComment(c, users))
	}

	return map[string]any{
		"ticket_id":   ticketID,
		"markdown":    md.String(),
		"attachments": attachments,
	}, nil
}

// getTicketUpdatesSince returns only comments created after the given timestamp.
func getTicketUpdatesSince(ticketID int, since string, includeInternal bool) (map[string]any, error) {
	sinceTime, err := time.Parse(time.RFC3339, since)
	if err != nil {
		return nil, fmt.Errorf("invalid 'since' timestamp (expected ISO 8601 / RFC 3339): %w", err)
	}

	comments, err := getAllTicketComments(ticketID)
	if err != nil {
		return nil, fmt.Errorf("fetching comments: %w", err)
	}

	// Filter to comments after the since timestamp
	var newComments []ZendeskComment
	var authorIDs []int
	for _, c := range comments {
		ct, err := time.Parse(time.RFC3339, c.CreatedAt)
		if err != nil {
			continue
		}
		if ct.After(sinceTime) {
			newComments = append(newComments, c)
			authorIDs = append(authorIDs, c.AuthorID)
		}
	}

	if len(newComments) == 0 {
		return map[string]any{
			"ticket_id":         ticketID,
			"has_updates":       false,
			"new_comment_count": 0,
			"markdown_append":   "",
			"attachments":       []ExportAttachment{},
		}, nil
	}

	users, err := getUsers(authorIDs)
	if err != nil {
		return nil, fmt.Errorf("resolving users: %w", err)
	}

	filtered := filterComments(newComments, includeInternal)
	attachments := processAttachments(filtered)

	var md strings.Builder
	for _, c := range filtered {
		md.WriteString("\n---\n\n")
		md.WriteString(renderComment(c, users))
	}

	// Include current ticket status so caller can update frontmatter if needed
	ticketRes, err := getTicket(ticketID)
	if err != nil {
		return nil, fmt.Errorf("fetching ticket status: %w", err)
	}

	result := map[string]any{
		"ticket_id":         ticketID,
		"has_updates":       len(filtered) > 0,
		"new_comment_count": len(filtered),
		"markdown_append":   md.String(),
		"attachments":       attachments,
		"current_status":    ticketRes.Ticket.Status,
	}
	if ticketRes.Ticket.Priority != nil {
		result["current_priority"] = *ticketRes.Ticket.Priority
	}

	return result, nil
}

// htmlBodyToMarkdown converts Zendesk's html_body to markdown using the
// html-to-markdown package. This handles tables, code blocks, links, images,
// lists, formatting, and all standard HTML elements.
func htmlBodyToMarkdown(htmlStr string) string {
	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(
				commonmark.WithCodeBlockFence("```"),
			),
			table.NewTablePlugin(),
		),
	)

	markdown, err := conv.ConvertString(htmlStr)
	if err != nil {
		return htmlStr
	}

	markdown = strings.ReplaceAll(markdown, "\u00A0", " ")
	return strings.TrimSpace(markdown)
}

// extractZendeskToken pulls the unique token path segment from a Zendesk
// attachment URL (e.g. "oLIuykejCvC12RY61XoTcxX6I" from the CDN URL).
func extractZendeskToken(contentURL string) string {
	// URL format: https://...zendesk.com/attachments/token/TOKEN_HERE/...
	if i := strings.Index(contentURL, "/token/"); i >= 0 {
		rest := contentURL[i+7:]
		if j := strings.Index(rest, "/"); j >= 0 {
			return rest[:j]
		}
		return rest
	}
	return ""
}

// replaceURLContaining finds markdown links whose URL contains the given
// substring and replaces the entire URL with the replacement.
func replaceURLContaining(body, urlSubstring, replacement string) string {
	// Match markdown link/image patterns: [text](url) or ![alt](url)
	re := regexp.MustCompile(`(!?\[[^\]]*\])\(([^)]*` + regexp.QuoteMeta(urlSubstring) + `[^)]*)\)`)
	return re.ReplaceAllString(body, "${1}("+replacement+")")
}
