package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

var nonAlphanumDot = regexp.MustCompile(`[^a-z0-9.]+`)

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

	for _, a := range comment.Attachments {
		if a.ContentURL != "" && a.FileName != "" {
			body = strings.ReplaceAll(body, a.ContentURL, fmt.Sprintf("./attachments/%s", sanitizeFilename(a.FileName)))
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

// htmlBodyToMarkdown converts Zendesk's html_body to markdown.
// The plain text body strips table formatting, so we parse the HTML to
// recover <table> elements as proper markdown tables while leaving the
// rest of the content as-is (Zendesk already stores most text as markdown).
func htmlBodyToMarkdown(htmlStr string) string {
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return htmlStr
	}
	var sb strings.Builder
	walkNode(&sb, doc)
	result := strings.TrimSpace(sb.String())
	result = strings.ReplaceAll(result, "\u00A0", " ")
	return convertIndentedCodeBlocks(result)
}

func walkNode(sb *strings.Builder, n *html.Node) {
	switch {
	case n.Type == html.ElementNode && n.Data == "table":
		sb.WriteString("\n")
		sb.WriteString(renderHTMLTable(n))
		sb.WriteString("\n")
		return
	case n.Type == html.ElementNode && n.Data == "br":
		sb.WriteString("\n")
	case n.Type == html.ElementNode && n.Data == "p":
		sb.WriteString("\n")
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walkNode(sb, c)
		}
		sb.WriteString("\n")
		return
	case n.Type == html.ElementNode && n.Data == "a":
		href := getAttr(n, "href")
		text := collectText(n)
		if href != "" && text != "" {
			sb.WriteString(fmt.Sprintf("[%s](%s)", text, href))
		} else if text != "" {
			sb.WriteString(text)
		}
		return
	case n.Type == html.ElementNode && n.Data == "img":
		src := getAttr(n, "src")
		alt := getAttr(n, "alt")
		if src != "" {
			sb.WriteString(fmt.Sprintf("![%s](%s)", alt, src))
		}
		return
	case n.Type == html.ElementNode && n.Data == "code":
		text := collectText(n)
		sb.WriteString("`" + text + "`")
		return
	case n.Type == html.ElementNode && n.Data == "pre":
		text := collectTextWithBreaks(n)
		sb.WriteString("\n```\n" + text + "\n```\n")
		return
	case n.Type == html.ElementNode && n.Data == "strong" || n.Data == "b":
		text := collectText(n)
		sb.WriteString("**" + text + "**")
		return
	case n.Type == html.ElementNode && n.Data == "em" || n.Data == "i":
		text := collectText(n)
		sb.WriteString("_" + text + "_")
		return
	case n.Type == html.ElementNode && (n.Data == "h1" || n.Data == "h2" || n.Data == "h3" || n.Data == "h4"):
		level := int(n.Data[1] - '0')
		text := collectText(n)
		sb.WriteString("\n" + strings.Repeat("#", level) + " " + text + "\n")
		return
	case n.Type == html.ElementNode && n.Data == "li":
		sb.WriteString("- ")
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walkNode(sb, c)
		}
		sb.WriteString("\n")
		return
	case n.Type == html.ElementNode && n.Data == "blockquote":
		text := collectText(n)
		for _, line := range strings.Split(text, "\n") {
			sb.WriteString("> " + line + "\n")
		}
		return
	case n.Type == html.ElementNode && n.Data == "div":
		// divs are just containers, walk children
	case n.Type == html.TextNode:
		sb.WriteString(n.Data)
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkNode(sb, c)
	}
}

func renderHTMLTable(table *html.Node) string {
	var rows [][]string
	forEachElement(table, "tr", func(tr *html.Node) {
		var cells []string
		for c := tr.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
				cells = append(cells, strings.TrimSpace(collectText(c)))
			}
		}
		if len(cells) > 0 {
			rows = append(rows, cells)
		}
	})

	if len(rows) == 0 {
		return ""
	}

	// Calculate column widths
	numCols := 0
	for _, row := range rows {
		if len(row) > numCols {
			numCols = len(row)
		}
	}
	widths := make([]int, numCols)
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	// Minimum width of 3 for separator
	for i := range widths {
		if widths[i] < 3 {
			widths[i] = 3
		}
	}

	var sb strings.Builder
	for ri, row := range rows {
		sb.WriteString("|")
		for ci := 0; ci < numCols; ci++ {
			cell := ""
			if ci < len(row) {
				cell = row[ci]
			}
			sb.WriteString(fmt.Sprintf(" %-*s |", widths[ci], cell))
		}
		sb.WriteString("\n")

		// Add separator after header row
		if ri == 0 {
			sb.WriteString("|")
			for ci := 0; ci < numCols; ci++ {
				sb.WriteString(" " + strings.Repeat("-", widths[ci]) + " |")
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func forEachElement(n *html.Node, tag string, fn func(*html.Node)) {
	if n.Type == html.ElementNode && n.Data == tag {
		fn(n)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		forEachElement(c, tag, fn)
	}
}

func collectText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(collectText(c))
	}
	return sb.String()
}

// collectTextWithBreaks is like collectText but converts <br> tags to newlines.
// Used inside <pre> blocks where <br> represents actual line breaks.
func collectTextWithBreaks(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	if n.Type == html.ElementNode && n.Data == "br" {
		return "\n"
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(collectTextWithBreaks(c))
	}
	return sb.String()
}

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// convertIndentedCodeBlocks replaces 4-space indented code blocks with
// fenced (```) code blocks for readability.
func convertIndentedCodeBlocks(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	inBlock := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		isIndented := strings.HasPrefix(line, "    ") && strings.TrimSpace(line) != ""
		isEmpty := strings.TrimSpace(line) == ""

		if isIndented && !inBlock {
			// Check that the previous line is blank or start of text
			if len(result) == 0 || strings.TrimSpace(result[len(result)-1]) == "" {
				inBlock = true
				result = append(result, "```")
				result = append(result, line[4:])
				continue
			}
		}

		if inBlock {
			if isIndented {
				result = append(result, line[4:])
				continue
			}
			// Blank lines within an indented block are kept if the next
			// non-blank line is still indented
			if isEmpty {
				next := peekNextNonEmpty(lines, i+1)
				if strings.HasPrefix(next, "    ") {
					result = append(result, "")
					continue
				}
			}
			// End of indented block
			inBlock = false
			result = append(result, "```")
		}

		result = append(result, line)
	}

	if inBlock {
		result = append(result, "```")
	}

	return strings.Join(result, "\n")
}

func peekNextNonEmpty(lines []string, from int) string {
	for i := from; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	return ""
}
