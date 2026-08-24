package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

// cstFixed is UTC+8 for cross-timezone tests where shanghaiLoc can't be loaded.
var cstFixed = time.FixedZone("CST", 8*3600)

func TestSaveAndLoadSpendLimitConfig(t *testing.T) {
	db := newSpendLimitTestStore(t)
	ctx := context.Background()

	cfg := SpendLimitConfig{
		Enabled: true,
		Default: SpendLimit{
			DailyCents:  15000,
			WeeklyCents: 50000,
		},
		Overrides: []SpendLimitEntry{
			{ApplyTo: "api-key", ApplyValue: "hash-a", DailyCents: 1000, WeeklyCents: 2000},
		},
	}

	if err := db.SaveSpendLimitConfig(ctx, cfg); err != nil {
		t.Fatalf("SaveSpendLimitConfig failed: %v", err)
	}

	loaded, ok, err := db.LoadSpendLimitConfig(ctx)
	if err != nil {
		t.Fatalf("LoadSpendLimitConfig failed: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true after save")
	}
	if !loaded.Enabled {
		t.Fatal("expected enabled=true")
	}
	if got := loaded.DefaultLimit(); got.DailyCents != 15000 || got.WeeklyCents != 50000 {
		t.Fatalf("default limit = %+v", got)
	}
	if len(loaded.Overrides) != 1 {
		t.Fatalf("len(overrides) = %d, want 1", len(loaded.Overrides))
	}
	limit := loaded.LimitForKey("hash-a")
	if limit.DailyCents != 1000 || limit.WeeklyCents != 2000 {
		t.Fatalf("override limit = %+v", limit)
	}
	limit = loaded.LimitForKey("hash-b")
	if limit.DailyCents != 15000 || limit.WeeklyCents != 50000 {
		t.Fatalf("fallback limit = %+v", limit)
	}
}

func TestSpendLimitOverrideMatchesFullAndShortAPIKeyHashes(t *testing.T) {
	cfg := SpendLimitConfig{
		Overrides: []SpendLimitEntry{{
			ApplyTo:    "api-key",
			ApplyValue: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
			DailyCents: 100,
		}},
	}

	limit, ok := cfg.OverrideForKey("abcdef01")
	if !ok || limit.DailyCents != 100 {
		t.Fatalf("short hash lookup = %+v, %v; want daily=100, true", limit, ok)
	}
	limit, ok = cfg.OverrideForKey("ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789")
	if !ok || limit.DailyCents != 100 {
		t.Fatalf("full hash lookup = %+v, %v; want daily=100, true", limit, ok)
	}
}

func TestQueryKeySpend_MatchesCostFormulaAndExcludesFailed(t *testing.T) {
	db := newSpendLimitTestStore(t)
	ctx := context.Background()
	seedSpendPrice(t, db)

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, cstFixed)
	insertSpendEvent(t, db, usage.Event{
		EventHash:    "base",
		TimestampMS:  now.UnixMilli(),
		Timestamp:    now.UTC().Format(time.RFC3339),
		Model:        "gpt-4",
		APIKeyHash:   "hash-spend",
		InputTokens:  100000,
		CachedTokens: 20000,
		OutputTokens: 10000,
		TotalTokens:  110000,
		CreatedAtMS:  now.UnixMilli(),
	})
	insertSpendEvent(t, db, usage.Event{
		EventHash:    "failed",
		TimestampMS:  now.UnixMilli(),
		Timestamp:    now.UTC().Format(time.RFC3339),
		Model:        "gpt-4",
		APIKeyHash:   "hash-spend",
		InputTokens:  1000000,
		OutputTokens: 1000000,
		TotalTokens:  2000000,
		Failed:       true,
		CreatedAtMS:  now.UnixMilli(),
	})

	result := findSpend(t, db, ctx, now, "hash-spend")
	if result.TodayCents != 120 || result.WeekCents != 120 {
		t.Fatalf("spend = %+v, want today/week 120 cents", result)
	}
}

func TestQueryKeySpendDoesNotMaterializeAllPricedEvents(t *testing.T) {
	db := newSpendLimitTestStore(t)
	rows, err := db.db.Query(`explain query plan `+keySpendWindowQuery, time.Now().UnixMilli(), time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("explain query plan failed: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan query plan: %v", err)
		}
		if strings.Contains(strings.ToLower(detail), "materialize priced_events") ||
			strings.Contains(strings.ToLower(detail), "co-routine priced_events") {
			t.Fatalf("spend query materializes the full priced_events CTE: %s", detail)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read query plan: %v", err)
	}
}

func TestQueryUserSpendDoesNotMaterializeAllPricedEvents(t *testing.T) {
	db := newSpendLimitTestStore(t)
	rows, err := db.db.Query(`explain query plan `+userSpendQuery, time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("explain user spend query plan failed: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan user spend query plan: %v", err)
		}
		if strings.Contains(strings.ToLower(detail), "materialize priced_events") ||
			strings.Contains(strings.ToLower(detail), "co-routine priced_events") {
			t.Fatalf("user spend query materializes the full priced_events CTE: %s", detail)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read user spend query plan: %v", err)
	}
}

func TestQueryKeySpend_UsesLargerCacheTokenField(t *testing.T) {
	db := newSpendLimitTestStore(t)
	ctx := context.Background()
	seedSpendPrice(t, db)

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, cstFixed)
	insertSpendEvent(t, db, usage.Event{
		EventHash:    "cache-token",
		TimestampMS:  now.UnixMilli(),
		Timestamp:    now.UTC().Format(time.RFC3339),
		Model:        "gpt-4",
		APIKeyHash:   "hash-cache",
		InputTokens:  100000,
		CachedTokens: 10000,
		CacheTokens:  50000,
		TotalTokens:  100000,
		CreatedAtMS:  now.UnixMilli(),
	})

	result := findSpend(t, db, ctx, now, "hash-cache")
	if result.TodayCents != 75 || result.WeekCents != 75 {
		t.Fatalf("spend = %+v, want today/week 75 cents", result)
	}
}

func TestQueryKeySpend_UsesLocalDayAndMondayWeek(t *testing.T) {
	db := newSpendLimitTestStore(t)
	ctx := context.Background()
	seedSpendPrice(t, db)

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, shanghaiLoc)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, shanghaiLoc)
	daysSinceMonday := (int(dayStart.Weekday()) + 6) % 7
	weekStart := dayStart.AddDate(0, 0, -daysSinceMonday)

	insertSpendEvent(t, db, spendEvent("today", "hash-window", dayStart.Add(time.Hour)))
	insertSpendEvent(t, db, spendEvent("week", "hash-window", weekStart.Add(time.Hour)))
	insertSpendEvent(t, db, spendEvent("old", "hash-window", weekStart.Add(-time.Millisecond)))

	result := findSpend(t, db, ctx, now, "hash-window")
	if result.TodayCents != 100 {
		t.Fatalf("today cents = %d, want 100", result.TodayCents)
	}
	if result.WeekCents != 200 {
		t.Fatalf("week cents = %d, want 200", result.WeekCents)
	}
}

func TestQueryKeySpend_CrossTimezoneBoundary(t *testing.T) {
	// Server runs in EDT (UTC-4). User is in Shanghai (UTC+8).
	// Insert event at Shanghai Day 1 12:00 (UTC 04:00).
	// Query at Shanghai Day 2 08:00 (UTC 00:00, EDT Day 1 20:00).
	// With shanghaiLoc, dayStart = Shanghai Day 2 00:00 = UTC Day 1 16:00.
	// The event at UTC 04:00 < 16:00 → NOT counted → todayCents=0.

	db := newSpendLimitTestStore(t)
	ctx := context.Background()
	seedSpendPrice(t, db)

	// Event at Shanghai Day 1 12:00 CST = UTC 04:00
	shanghaiNoon := time.Date(2026, 6, 29, 12, 0, 0, 0, cstFixed)
	insertSpendEvent(t, db, usage.Event{
		EventHash:   "tz-base",
		TimestampMS: shanghaiNoon.UnixMilli(),
		Timestamp:   shanghaiNoon.UTC().Format(time.RFC3339),
		Model:       "gpt-4",
		APIKeyHash:  "hash-tz",
		InputTokens: 100000,
		TotalTokens: 100000,
		CreatedAtMS: shanghaiNoon.UnixMilli(),
	})

	// Query at Shanghai Day 2 08:00 CST = UTC 00:00
	// This is the time when the user reported seeing old consumption.
	shanghaiDay2Morning := time.Date(2026, 6, 30, 8, 0, 0, 0, cstFixed)
	results, err := db.queryKeySpendAt(ctx, shanghaiDay2Morning)
	if err != nil {
		t.Fatalf("queryKeySpendAt failed: %v", err)
	}

	for _, r := range results {
		if r.KeyHash == "hash-tz" {
			if r.TodayCents != 0 {
				t.Fatalf("expected todayCents=0 at Shanghai Day2 08:00, got %d", r.TodayCents)
			}
			// Event is in the same week → weekCents should be > 0
			if r.WeekCents <= 0 {
				t.Fatalf("expected weekCents>0 (event is same week), got %d", r.WeekCents)
			}
			return
		}
	}
	t.Fatal("key hash-tz not found in results (expected in week range)")
}

func TestQueryUserSpend_CrossTimezoneBoundary(t *testing.T) {
	db := newSpendLimitTestStore(t)
	ctx := context.Background()

	// Seed departments and key bindings so QueryUserSpend can resolve user spend
	if err := db.UpsertEnterpriseDepartments(ctx, []EnterpriseDepartment{
		{ID: "dept_sh", Name: "上海总部", Prefix: "sh", SortOrder: 1, Enabled: true},
	}); err != nil {
		t.Fatalf("upsert departments: %v", err)
	}
	if err := db.UpsertEnterpriseKeyBindings(ctx, []EnterpriseKeyBinding{
		{
			APIKey:               "key-user-tz",
			UserName:             "tzuser",
			DepartmentID:         "dept_sh",
			Email:                "tz@example.com",
			Source:               "manual",
			DepartmentResolvedBy: "manual",
		},
	}); err != nil {
		t.Fatalf("upsert key bindings: %v", err)
	}
	// Resolve the actual APIKeyHash
	bindings, err := db.LoadEnterpriseKeyBindings(ctx)
	if err != nil {
		t.Fatalf("load bindings: %v", err)
	}
	var hashTZ string
	for _, b := range bindings {
		if b.UserName == "tzuser" {
			hashTZ = b.APIKeyHash
		}
	}
	if hashTZ == "" {
		t.Fatalf("could not resolve binding hash for tzuser")
	}

	seedSpendPrice(t, db)

	// Event at Shanghai Day 1 12:00 CST = UTC 04:00
	shanghaiNoon := time.Date(2026, 6, 29, 12, 0, 0, 0, cstFixed)
	_, err = db.InsertEvents(ctx, []usage.Event{{
		EventHash:   "user-tz-1",
		TimestampMS: shanghaiNoon.UnixMilli(),
		Timestamp:   shanghaiNoon.UTC().Format(time.RFC3339),
		Model:       "gpt-4",
		APIKeyHash:  hashTZ,
		InputTokens: 100000,
		TotalTokens: 100000,
		CreatedAtMS: shanghaiNoon.UnixMilli(),
	}})
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}

	// Query user spend at Shanghai Day 2 08:00 CST = UTC 00:00
	// Yesterday's events must NOT appear in today's consumption.
	users, err := db.QueryUserSpend(ctx)
	if err != nil {
		t.Fatalf("QueryUserSpend failed: %v", err)
	}
	for _, u := range users {
		if u.UserName == "tzuser" {
			t.Fatalf("expected no spend for tzuser at Shanghai Day2 08:00, but got todayCents=%d", u.TodayCents)
		}
	}
}

func newSpendLimitTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedSpendPrice(t *testing.T, db *Store) {
	t.Helper()
	_, err := db.UpsertSyncedModelPrices(context.Background(), map[string]ModelPrice{
		"gpt-4": {Prompt: 10, Completion: 30, Cache: 5},
	})
	if err != nil {
		t.Fatalf("UpsertSyncedModelPrices failed: %v", err)
	}
}

func insertSpendEvent(t *testing.T, db *Store, event usage.Event) {
	t.Helper()
	if _, err := db.InsertEvents(context.Background(), []usage.Event{event}); err != nil {
		t.Fatalf("InsertEvents failed: %v", err)
	}
}

func spendEvent(hash, keyHash string, at time.Time) usage.Event {
	return usage.Event{
		EventHash:   hash,
		TimestampMS: at.UnixMilli(),
		Timestamp:   at.UTC().Format(time.RFC3339),
		Model:       "gpt-4",
		APIKeyHash:  keyHash,
		InputTokens: 100000,
		TotalTokens: 100000,
		CreatedAtMS: at.UnixMilli(),
	}
}

func findSpend(t *testing.T, db *Store, ctx context.Context, now time.Time, keyHash string) KeySpend {
	t.Helper()
	results, err := db.queryKeySpendAt(ctx, now)
	if err != nil {
		t.Fatalf("queryKeySpendAt failed: %v", err)
	}
	for _, result := range results {
		if result.KeyHash == keyHash {
			return result
		}
	}
	t.Fatalf("key %q not found in %+v", keyHash, results)
	return KeySpend{}
}
