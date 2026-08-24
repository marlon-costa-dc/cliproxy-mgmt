package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

// TestZeroTokenFailedEventsAreIncluded verifies that after commit 13f9d67
// removed the SQL filter, failed zero-token events appear in usage reports.
func TestZeroTokenFailedEventsAreIncluded(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	if _, err := db.InsertEvents(ctx, []usage.Event{
		{
			EventHash:   "test-zero-fail",
			TimestampMS: 1_700_000_000_000,
			Timestamp:   "2023-11-14T22:13:20Z",
			Model:       "gpt-zero",
			Endpoint:    "POST /v1/chat/completions",
			APIKeyHash:  "hash-a",
			Failed:      true,
			TotalTokens: 0,
			CreatedAtMS: 1_700_000_000_001,
		},
	}); err != nil {
		t.Fatalf("InsertEvents failed: %v", err)
	}

	rows, err := db.UsageReport(ctx, 1_699_000_000_000, 1_701_000_000_000)
	if err != nil {
		t.Fatalf("UsageReport failed: %v", err)
	}

	found := false
	for _, r := range rows {
		for _, m := range r.Models {
			if m.Model == "gpt-zero" {
				found = true
				if m.FailedRequests != 1 {
					t.Fatalf("gpt-zero FailedRequests = %d, want 1", m.FailedRequests)
				}
				if m.Requests != 1 {
					t.Fatalf("gpt-zero Requests = %d, want 1", m.Requests)
				}
			}
		}
	}
	if !found {
		t.Fatal("gpt-zero should be included as a failed zero-token event after spec change")
	}
}
