package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

var (
	zendeskSubdomain = os.Getenv("ZENDESK_SUBDOMAIN")
	zendeskCookie    = os.Getenv("ZENDESK_COOKIE")
)

func getBaseURL() (string, error) {
	if zendeskSubdomain == "" {
		return "", fmt.Errorf("ZENDESK_SUBDOMAIN environment variable is required")
	}
	return fmt.Sprintf("https://%s.zendesk.com/api/v2", zendeskSubdomain), nil
}

func getCookie() (string, error) {
	cookieMu.Lock()
	cookie := zendeskCookie
	cookieMu.Unlock()
	if cookie == "" {
		return "", fmt.Errorf("ZENDESK_COOKIE not set and browser cookie extraction failed")
	}
	return cookie, nil
}

// zendeskFetch is the core HTTP helper. Every Zendesk API call goes through here.
func zendeskFetch(path string, params map[string]string) ([]byte, error) {
	baseURL, err := getBaseURL()
	if err != nil {
		return nil, err
	}

	cookie, err := getCookie()
	if err != nil {
		return nil, err
	}

	u, err := url.Parse(baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	if params != nil {
		q := u.Query()
		for k, v := range params {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Cookie", cookie)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// On 401, try refreshing the cookie from the browser and retry once
		if resp.StatusCode == 401 {
			newCookie, refreshErr := refreshCookie()
			if refreshErr == nil && newCookie != cookie {
				req2, _ := http.NewRequest("GET", u.String(), nil)
				req2.Header.Set("Cookie", newCookie)
				req2.Header.Set("Content-Type", "application/json")
				req2.Header.Set("Accept", "application/json")
				resp2, err2 := http.DefaultClient.Do(req2)
				if err2 == nil {
					defer resp2.Body.Close()
					if resp2.StatusCode >= 200 && resp2.StatusCode < 300 {
						return io.ReadAll(resp2.Body)
					}
				}
			}
		}
		return nil, fmt.Errorf("Zendesk API error %d: %s\n%s", resp.StatusCode, resp.Status, string(body))
	}

	return body, nil
}

// Zendesk API types

type ZendeskTicket struct {
	ID             int           `json:"id"`
	Subject        string        `json:"subject"`
	Description    string        `json:"description"`
	Status         string        `json:"status"`
	Priority       *string       `json:"priority"`
	Type           *string       `json:"type"`
	CreatedAt      string        `json:"created_at"`
	UpdatedAt      string        `json:"updated_at"`
	RequesterID    int           `json:"requester_id"`
	AssigneeID     *int          `json:"assignee_id"`
	OrganizationID *int          `json:"organization_id"`
	GroupID        *int          `json:"group_id"`
	Tags           []string      `json:"tags"`
	CustomFields   []CustomField `json:"custom_fields"`
	URL            string        `json:"url"`
}

type CustomField struct {
	ID    int `json:"id"`
	Value any `json:"value"`
}

type ZendeskComment struct {
	ID          int          `json:"id"`
	Body        string       `json:"body"`
	HTMLBody    string       `json:"html_body"`
	AuthorID    int          `json:"author_id"`
	CreatedAt   string       `json:"created_at"`
	Public      bool         `json:"public"`
	Attachments []Attachment `json:"attachments"`
}

type Attachment struct {
	FileName   string `json:"file_name"`
	ContentURL string `json:"content_url"`
	Size       int    `json:"size"`
}

// API functions

type searchResult struct {
	Results []ZendeskTicket `json:"results"`
	Count   int             `json:"count"`
}

type ticketsResult struct {
	Tickets []ZendeskTicket `json:"tickets"`
	Count   int             `json:"count"`
}

type ticketResult struct {
	Ticket ZendeskTicket `json:"ticket"`
}

type commentsResult struct {
	Comments []ZendeskComment `json:"comments"`
	Count    int              `json:"count"`
}

type ticketListResult struct {
	Tickets []ZendeskTicket
	Count   int
}

func searchTickets(query string, page, perPage int) (*ticketListResult, error) {
	data, err := zendeskFetch("/search.json", map[string]string{
		"query":      "type:ticket " + query,
		"page":       strconv.Itoa(page),
		"per_page":   strconv.Itoa(perPage),
		"sort_by":    "updated_at",
		"sort_order": "desc",
	})
	if err != nil {
		return nil, err
	}

	var result searchResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &ticketListResult{Tickets: result.Results, Count: result.Count}, nil
}

func getTicket(ticketID int) (*ticketResult, error) {
	data, err := zendeskFetch(fmt.Sprintf("/tickets/%d.json", ticketID), nil)
	if err != nil {
		return nil, err
	}

	var result ticketResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &result, nil
}

func getTicketComments(ticketID, page, perPage int) (*commentsResult, error) {
	data, err := zendeskFetch(fmt.Sprintf("/tickets/%d/comments.json", ticketID), map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	})
	if err != nil {
		return nil, err
	}

	var result commentsResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &result, nil
}

func listTickets(status string, page, perPage int) (*ticketListResult, error) {
	if status != "" {
		return searchTickets("status:"+status, page, perPage)
	}

	data, err := zendeskFetch("/tickets.json", map[string]string{
		"page":       strconv.Itoa(page),
		"per_page":   strconv.Itoa(perPage),
		"sort_by":    "updated_at",
		"sort_order": "desc",
	})
	if err != nil {
		return nil, err
	}

	var result ticketsResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &ticketListResult{Tickets: result.Tickets, Count: result.Count}, nil
}

// User and organization types

type ZendeskUser struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type usersResult struct {
	Users []ZendeskUser `json:"users"`
}

type ZendeskOrganization struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type organizationResult struct {
	Organization ZendeskOrganization `json:"organization"`
}

// getUsers fetches multiple users by ID in a single batch API call.
// Zendesk's /users/show_many.json endpoint accepts up to 100 IDs.
func getUsers(ids []int) (map[int]ZendeskUser, error) {
	if len(ids) == 0 {
		return map[int]ZendeskUser{}, nil
	}

	seen := make(map[int]bool)
	var unique []string
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, strconv.Itoa(id))
		}
	}

	data, err := zendeskFetch("/users/show_many.json", map[string]string{
		"ids": strings.Join(unique, ","),
	})
	if err != nil {
		return nil, err
	}

	var result usersResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing users response: %w", err)
	}

	userMap := make(map[int]ZendeskUser, len(result.Users))
	for _, u := range result.Users {
		userMap[u.ID] = u
	}
	return userMap, nil
}

func getOrganization(orgID int) (*ZendeskOrganization, error) {
	data, err := zendeskFetch(fmt.Sprintf("/organizations/%d.json", orgID), nil)
	if err != nil {
		return nil, err
	}

	var result organizationResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing org response: %w", err)
	}

	return &result.Organization, nil
}

// getAllTicketComments fetches every comment on a ticket, paginating automatically.
func getAllTicketComments(ticketID int) ([]ZendeskComment, error) {
	var all []ZendeskComment
	page := 1
	for {
		result, err := getTicketComments(ticketID, page, 100)
		if err != nil {
			return nil, err
		}
		all = append(all, result.Comments...)
		if len(all) >= result.Count {
			break
		}
		page++
	}
	return all, nil
}

// downloadAttachment fetches attachment bytes from a Zendesk CDN URL.
func downloadAttachment(contentURL string) ([]byte, error) {
	cookie, err := getCookie()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", contentURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating attachment request: %w", err)
	}
	req.Header.Set("Cookie", cookie)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading attachment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("attachment download error %d: %s", resp.StatusCode, resp.Status)
	}

	return io.ReadAll(resp.Body)
}
