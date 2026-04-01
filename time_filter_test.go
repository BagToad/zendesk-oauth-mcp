package main

import (
	"testing"
)

func TestTimeFilterQueryConstruction(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		createdAfter string
		updatedAfter string
		wantContains []string
		wantMissing  []string
	}{
		{
			name:         "no time filters",
			query:        "status:open",
			createdAfter: "",
			updatedAfter: "",
			wantContains: []string{"status:open"},
			wantMissing:  []string{"created>", "updated>"},
		},
		{
			name:         "created_after only with ISO 8601",
			query:        "status:open",
			createdAfter: "2026-04-01T08:00:00Z",
			updatedAfter: "",
			wantContains: []string{"status:open", "created>2026-04-01T08:00:00Z"},
			wantMissing:  []string{"updated>"},
		},
		{
			name:         "updated_after only with ISO 8601",
			query:        "priority:high",
			createdAfter: "",
			updatedAfter: "2026-03-31T00:00:00Z",
			wantContains: []string{"priority:high", "updated>2026-03-31T00:00:00Z"},
			wantMissing:  []string{"created>"},
		},
		{
			name:         "both time filters",
			query:        "cat_github_actions",
			createdAfter: "2026-04-01T06:00:00Z",
			updatedAfter: "2026-04-01T12:00:00Z",
			wantContains: []string{"cat_github_actions", "created>2026-04-01T06:00:00Z", "updated>2026-04-01T12:00:00Z"},
		},
		{
			name:         "date-only format",
			query:        "status:new",
			createdAfter: "2026-04-01",
			updatedAfter: "",
			wantContains: []string{"status:new", "created>2026-04-01"},
		},
		{
			name:         "empty query with time filters",
			query:        "",
			createdAfter: "2026-04-01T00:00:00Z",
			updatedAfter: "",
			wantContains: []string{"created>2026-04-01T00:00:00Z"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the query construction logic from handleSearchTickets.
			query := tt.query
			if tt.createdAfter != "" {
				query += " created>" + tt.createdAfter
			}
			if tt.updatedAfter != "" {
				query += " updated>" + tt.updatedAfter
			}

			for _, want := range tt.wantContains {
				if !contains(query, want) {
					t.Errorf("query = %q, want to contain %q", query, want)
				}
			}
			for _, missing := range tt.wantMissing {
				if contains(query, missing) {
					t.Errorf("query = %q, should NOT contain %q", query, missing)
				}
			}
		})
	}
}

func TestTicketSummaryIncludesCreatedAt(t *testing.T) {
	tickets := []ZendeskTicket{
		{
			ID:        12345,
			Subject:   "Test ticket",
			Status:    "open",
			CreatedAt: "2026-04-01T08:00:00Z",
			UpdatedAt: "2026-04-01T12:00:00Z",
			Tags:      []string{"cat_github_actions"},
		},
	}

	summaries := toSummaries(tickets)

	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].CreatedAt != "2026-04-01T08:00:00Z" {
		t.Errorf("CreatedAt = %q, want 2026-04-01T08:00:00Z", summaries[0].CreatedAt)
	}
	if summaries[0].UpdatedAt != "2026-04-01T12:00:00Z" {
		t.Errorf("UpdatedAt = %q, want 2026-04-01T12:00:00Z", summaries[0].UpdatedAt)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
