package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

func TestStorePersistsAccountSnapshot(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	_, err = db.InsertEvents(context.Background(), []usage.Event{
		{
			EventHash:            "event-1",
			TimestampMS:          1_778_000_000_000,
			Timestamp:            "2026-05-06T00:00:00Z",
			Model:                "gpt-test",
			Endpoint:             "POST /v1/chat/completions",
			AuthIndex:            "auth-1",
			APIKeyHash:           "api-key-hash-1",
			AccountSnapshot:      "alice@example.com",
			AuthLabelSnapshot:    "Alice",
			AuthFileSnapshot:     "alice.json",
			AuthProviderSnapshot: "codex",
			AuthSnapshotAtMS:     1_778_000_000_100,
			CreatedAtMS:          1_778_000_000_200,
		},
	})
	if err != nil {
		t.Fatalf("insert events: %v", err)
	}

	events, err := db.RecentEvents(context.Background(), 10)
	if err != nil {
		t.Fatalf("recent events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	event := events[0]
	if event.AccountSnapshot != "alice@example.com" {
		t.Fatalf("AccountSnapshot = %q", event.AccountSnapshot)
	}
	if event.AuthLabelSnapshot != "Alice" {
		t.Fatalf("AuthLabelSnapshot = %q", event.AuthLabelSnapshot)
	}
	if event.AuthFileSnapshot != "alice.json" {
		t.Fatalf("AuthFileSnapshot = %q", event.AuthFileSnapshot)
	}
	if event.AuthProviderSnapshot != "codex" {
		t.Fatalf("AuthProviderSnapshot = %q", event.AuthProviderSnapshot)
	}
	if event.AuthSnapshotAtMS != 1_778_000_000_100 {
		t.Fatalf("AuthSnapshotAtMS = %d", event.AuthSnapshotAtMS)
	}
	if event.APIKeyHash != "api-key-hash-1" {
		t.Fatalf("APIKeyHash = %q", event.APIKeyHash)
	}

	payload := usage.BuildPayload(events)
	detail := payload.APIs["POST /v1/chat/completions"].Models["gpt-test"].Details[0]
	if detail.APIKeyHash != "api-key-hash-1" {
		t.Fatalf("payload APIKeyHash = %q", detail.APIKeyHash)
	}
	if detail.AccountSnapshot != "alice@example.com" {
		t.Fatalf("payload AccountSnapshot = %q", detail.AccountSnapshot)
	}
	if detail.AuthProviderSnapshot != "codex" {
		t.Fatalf("payload AuthProviderSnapshot = %q", detail.AuthProviderSnapshot)
	}
}

func TestStoreAPIKeyAliases(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	const hash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := db.UpsertAPIKeyAliases(context.Background(), []APIKeyAlias{
		{APIKeyHash: hash, Alias: " Alice "},
	}); err != nil {
		t.Fatalf("upsert alias: %v", err)
	}

	aliases, err := db.LoadAPIKeyAliases(context.Background())
	if err != nil {
		t.Fatalf("load aliases: %v", err)
	}
	if len(aliases) != 1 {
		t.Fatalf("len(aliases) = %d, want 1", len(aliases))
	}
	if aliases[0].APIKeyHash != hash || aliases[0].Alias != "Alice" || aliases[0].UpdatedAtMS <= 0 {
		t.Fatalf("alias = %#v", aliases[0])
	}

	if err := db.UpsertAPIKeyAliases(context.Background(), []APIKeyAlias{
		{APIKeyHash: hash, Alias: "Team A"},
	}); err != nil {
		t.Fatalf("update alias: %v", err)
	}
	aliases, err = db.LoadAPIKeyAliases(context.Background())
	if err != nil {
		t.Fatalf("reload aliases: %v", err)
	}
	if len(aliases) != 1 || aliases[0].Alias != "Team A" {
		t.Fatalf("updated aliases = %#v", aliases)
	}

	const otherHash = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	if err := db.UpsertAPIKeyAliases(context.Background(), []APIKeyAlias{
		{APIKeyHash: otherHash, Alias: " team a "},
	}); err == nil || err.Error() != "api key alias already exists" {
		t.Fatalf("duplicate alias error = %v", err)
	}
	if err := db.UpsertAPIKeyAliases(context.Background(), []APIKeyAlias{
		{APIKeyHash: hash, Alias: "Alpha"},
		{APIKeyHash: otherHash, Alias: " alpha "},
	}); err == nil || err.Error() != "api key alias already exists" {
		t.Fatalf("batch duplicate alias error = %v", err)
	}

	if err := db.DeleteAPIKeyAlias(context.Background(), hash); err != nil {
		t.Fatalf("delete alias: %v", err)
	}
	aliases, err = db.LoadAPIKeyAliases(context.Background())
	if err != nil {
		t.Fatalf("load after delete: %v", err)
	}
	if len(aliases) != 0 {
		t.Fatalf("aliases after delete = %#v", aliases)
	}
}

func TestStoreEnterpriseMetadataPersistence(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	ctx := context.Background()
	if err := db.UpsertEnterpriseDepartments(ctx, []EnterpriseDepartment{
		{ID: "ungrouped", Name: "未分组", SortOrder: 0, Enabled: true, System: true, UpdatedBy: "system"},
		{ID: "dept_sh", Name: "上海总部", Prefix: "sh", SortOrder: 1, Enabled: true, UpdatedBy: "admin"},
	}); err != nil {
		t.Fatalf("upsert departments: %v", err)
	}

	departments, err := db.LoadEnterpriseDepartments(ctx)
	if err != nil {
		t.Fatalf("load departments: %v", err)
	}
	if len(departments) < 2 {
		t.Fatalf("len(departments) = %d, want >= 2", len(departments))
	}
	var foundShanghai bool
	for _, department := range departments {
		if department.ID == "dept_sh" && department.Prefix == "sh" {
			foundShanghai = true
			break
		}
	}
	if !foundShanghai {
		t.Fatalf("departments missing dept_sh: %#v", departments)
	}

	if err := db.UpsertEnterpriseKeyBindings(ctx, []EnterpriseKeyBinding{
		{
			APIKey:               "sh-abc123",
			UserName:             "zhangsan",
			DepartmentID:         "dept_sh",
			Source:               "import",
			DepartmentResolvedBy: "import",
			UpdatedBy:            "admin",
		},
	}); err != nil {
		t.Fatalf("upsert key bindings: %v", err)
	}

	bindings, err := db.LoadEnterpriseKeyBindings(ctx)
	if err != nil {
		t.Fatalf("load key bindings: %v", err)
	}
	if len(bindings) != 1 {
		t.Fatalf("len(bindings) = %d, want 1", len(bindings))
	}
	if bindings[0].APIKey != "sh-abc123" || bindings[0].DepartmentID != "dept_sh" {
		t.Fatalf("binding = %#v", bindings[0])
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(bindings[0].APIKeyHash) {
		t.Fatalf("binding APIKeyHash = %q", bindings[0].APIKeyHash)
	}
	expectedHash := fmt.Sprintf("%x", sha256.Sum256([]byte(bindings[0].APIKey)))
	if bindings[0].APIKeyHash != expectedHash {
		t.Fatalf("binding APIKeyHash = %q, want %q", bindings[0].APIKeyHash, expectedHash)
	}

	aliases, err := db.LoadAPIKeyAliases(ctx)
	if err != nil {
		t.Fatalf("load aliases: %v", err)
	}
	if len(aliases) != 1 || aliases[0].Alias != "zhangsan" || aliases[0].APIKeyHash != bindings[0].APIKeyHash {
		t.Fatalf("aliases = %#v", aliases)
	}

	if err := db.AppendEnterpriseImportHistory(ctx, EnterpriseImportHistory{
		TaskID:       "task-001",
		CSVFileName:  "enterprise-users.csv",
		TotalRows:    10,
		PassedRows:   9,
		WarningRows:  1,
		ErrorRows:    0,
		ErrorDetails: `[{"row":4,"reason":"duplicate user"}]`,
		Status:       "completed",
		UpdatedBy:    "admin",
	}); err != nil {
		t.Fatalf("append import history: %v", err)
	}

	history, err := db.LoadEnterpriseImportHistory(ctx, 10)
	if err != nil {
		t.Fatalf("load import history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("len(history) = %d, want 1", len(history))
	}
	if history[0].TaskID != "task-001" || history[0].Status != "completed" {
		t.Fatalf("history = %#v", history[0])
	}
	if history[0].CSVFileName != "enterprise-users.csv" {
		t.Fatalf("history CSVFileName = %q", history[0].CSVFileName)
	}
	if history[0].ErrorDetails != `[{"row":4,"reason":"duplicate user"}]` {
		t.Fatalf("history ErrorDetails = %q", history[0].ErrorDetails)
	}
}

func TestStoreEnterpriseSchemaMigrationIsIdempotent(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if err := db.ensureEnterpriseSchema(); err != nil {
		t.Fatalf("ensure schema first run: %v", err)
	}
	if err := db.ensureEnterpriseSchema(); err != nil {
		t.Fatalf("ensure schema second run: %v", err)
	}

	var count int
	if err := db.db.QueryRow(`select count(*) from pragma_table_info('enterprise_key_bindings') where name = 'email'`).Scan(&count); err != nil {
		t.Fatalf("query email column: %v", err)
	}
	if count != 1 {
		t.Fatalf("email column count = %d, want 1", count)
	}
}

func TestStoreEnterpriseKeyBindingsPersistEmail(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	ctx := context.Background()
	if err := db.UpsertEnterpriseDepartments(ctx, []EnterpriseDepartment{{
		ID:        "dept_sh",
		Name:      "上海总部",
		Prefix:    "sh",
		SortOrder: 1,
		Enabled:   true,
	}}); err != nil {
		t.Fatalf("upsert departments: %v", err)
	}

	if err := db.UpsertEnterpriseKeyBindings(ctx, []EnterpriseKeyBinding{{
		APIKey:               "sh-abc123",
		UserName:             "zhangsan",
		DepartmentID:         "dept_sh",
		Email:                "  zs@example.com  ",
		Source:               "manual",
		DepartmentResolvedBy: "manual",
	}}); err != nil {
		t.Fatalf("create key binding: %v", err)
	}

	bindings, err := db.LoadEnterpriseKeyBindings(ctx)
	if err != nil {
		t.Fatalf("load created key bindings: %v", err)
	}
	if len(bindings) != 1 {
		t.Fatalf("len(bindings) after create = %d, want 1", len(bindings))
	}
	if bindings[0].Email != "zs@example.com" {
		t.Fatalf("created binding email = %q, want zs@example.com", bindings[0].Email)
	}

	createdAt := bindings[0].CreatedAtMS
	if err := db.UpsertEnterpriseKeyBindings(ctx, []EnterpriseKeyBinding{{
		APIKey:               "sh-abc123",
		UserName:             "zhangsan",
		DepartmentID:         "dept_sh",
		Email:                "updated@example.com",
		Source:               "manual",
		DepartmentResolvedBy: "manual",
		CreatedAtMS:          createdAt,
	}}); err != nil {
		t.Fatalf("update key binding: %v", err)
	}

	bindings, err = db.LoadEnterpriseKeyBindings(ctx)
	if err != nil {
		t.Fatalf("load updated key bindings: %v", err)
	}
	if len(bindings) != 1 {
		t.Fatalf("len(bindings) after update = %d, want 1", len(bindings))
	}
	if bindings[0].Email != "updated@example.com" {
		t.Fatalf("updated binding email = %q, want updated@example.com", bindings[0].Email)
	}
}


func TestStoreUsageReport(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()

	// Seed departments and key bindings
	if err := db.UpsertEnterpriseDepartments(ctx, []EnterpriseDepartment{
		{ID: "dept_sh", Name: "上海总部", Prefix: "sh", SortOrder: 1, Enabled: true},
	}); err != nil {
		t.Fatalf("upsert departments: %v", err)
	}
	if err := db.UpsertEnterpriseKeyBindings(ctx, []EnterpriseKeyBinding{
		{
			APIKey:               "key-zhangsan",
			UserName:             "zhangsan",
			DepartmentID:         "dept_sh",
			Email:                "zs@example.com",
			Source:               "manual",
			DepartmentResolvedBy: "manual",
		},
		{
			APIKey:               "key-lisi",
			UserName:             "lisi",
			DepartmentID:         "dept_sh",
			Email:                "lis@example.com",
			Source:               "manual",
			DepartmentResolvedBy: "manual",
		},
	}); err != nil {
		t.Fatalf("upsert key bindings: %v", err)
	}
	// Look up the generated binding keys
	bindings, err := db.LoadEnterpriseKeyBindings(ctx)
	if err != nil {
		t.Fatalf("load bindings: %v", err)
	}
	var keyA, keyB string
	var hashA, hashB string
	for _, b := range bindings {
		switch b.UserName {
		case "zhangsan":
			keyA = b.APIKey
			hashA = b.APIKeyHash
		case "lisi":
			keyB = b.APIKey
			hashB = b.APIKeyHash
		}
	}
	if keyA == "" || keyB == "" {
		t.Fatalf("could not resolve binding keys: %#v", bindings)
	}
	// Seed usage events — hashA has two models, hashB has one
	if _, err := db.InsertEvents(ctx, []usage.Event{
		{
			EventHash:    "e-a1",
			TimestampMS:  1_700_000_000_000,
			Timestamp:    "2023-11-14T22:13:20Z",
			Model:        "gpt-4",
			Endpoint:     "POST /v1/chat/completions",
			APIKeyHash:   hashA,
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  100,
			CachedTokens: 10,
			CacheTokens:  20,
			CreatedAtMS:  1_700_000_000_001,
		},
		{
			EventHash:   "e-a2",
			TimestampMS: 1_700_100_000_000,
			Timestamp:   "2023-11-16T02:00:00Z",
			Model:       "gpt-4",
			Endpoint:    "POST /v1/chat/completions",
			APIKeyHash:  hashA,
			TotalTokens: 200,
			Failed:      true,
			CreatedAtMS: 1_700_100_000_001,
		},
		{
			EventHash:    "e-a3",
			TimestampMS:  1_700_050_000_000,
			Timestamp:    "2023-11-15T12:00:00Z",
			Model:        "gpt-3.5-turbo",
			Endpoint:     "POST /v1/chat/completions",
			APIKeyHash:   hashA,
			InputTokens:  5,
			OutputTokens: 10,
			TotalTokens:  50,
			CachedTokens: 5,
			CacheTokens:  10,
			CreatedAtMS:  1_700_050_000_001,
		},
		{
			EventHash:    "e-b1",
			TimestampMS:  1_700_200_000_000,
			Timestamp:    "2023-11-17T12:00:00Z",
			Model:        "gpt-4",
			Endpoint:     "POST /v1/chat/completions",
			APIKeyHash:   hashB,
			InputTokens:  30,
			OutputTokens: 40,
			TotalTokens:  300,
			CachedTokens: 30,
			CacheTokens:  30,
			CreatedAtMS:  1_700_200_000_001,
		},
	}); err != nil {
		t.Fatalf("insert events: %v", err)
	}

		t.Run("happy path aggregates correctly", func(t *testing.T) {
			rows, err := db.UsageReport(ctx, 1_699_000_000_000, 1_701_000_000_000)
			if err != nil {
				t.Fatalf("usage report: %v", err)
			}
			// Expect 2 rows: lisi (alphabetically first) then zhangsan
			if len(rows) != 2 {
				t.Fatalf("len(rows) = %d, want 2", len(rows))
			}

			// First row: lisi (department_name=上海总部, user_name=lisi)
			row0 := rows[0]
			if row0.UserName != "lisi" || row0.DepartmentName != "上海总部" || row0.Email != "lis@example.com" {
				t.Fatalf("row0 user/dept/email = %q/%q/%q", row0.UserName, row0.DepartmentName, row0.Email)
			}
			if row0.APIKey != keyB {
				t.Fatalf("row0 apiKeyHash = %q, want %q", row0.APIKey, hashB)
			}
			if len(row0.Models) != 1 {
				t.Fatalf("row0 models = %d, want 1", len(row0.Models))
			}
			if row0.Models[0].Model != "gpt-4" {
				t.Fatalf("row0 model[0] = %q, want gpt-4", row0.Models[0].Model)
			}
			m0 := row0.Models[0]
			if m0.TotalTokens != 300 || m0.Requests != 1 || m0.FailedRequests != 0 {
				t.Fatalf("row0 gpt-4 stats = %+v", m0)
			}
			if m0.CachedTokens != 30 || m0.TotalCacheTokens != 30 {
				t.Fatalf("row0 cache stats = %+v", m0)
			}
			if m0.CacheHitRate != 1.0 {
				t.Fatalf("row0 gpt-4 cacheHitRate = %f, want 1.0", m0.CacheHitRate)
			}
			if row0.TotalTokens != 300 || row0.TotalRequests != 1 || row0.FailedRequests != 0 {
				t.Fatalf("row0 total aggregates = %+v", row0)
			}
			if row0.CachedTokens != 30 || row0.TotalCacheTokens != 30 {
				t.Fatalf("row0 cache aggregates = %+v", row0)
			}
			if row0.CacheHitRate != 1.0 {
				t.Fatalf("row0 cacheHitRate = %f, want 1.0", row0.CacheHitRate)
			}

			// Second row: zhangsan
			row1 := rows[1]
			if row1.UserName != "zhangsan" || row1.Email != "zs@example.com" {
				t.Fatalf("row1 user/email = %q/%q", row1.UserName, row1.Email)
			}
			if row1.APIKey != keyA {
				t.Fatalf("row1 apiKey = %q, want %q", row1.APIKey, hashA)
			}
			if len(row1.Models) != 2 {
				t.Fatalf("row1 models = %d, want 2", len(row1.Models))
			}
			if row1.Models[0].Model != "gpt-3.5-turbo" || row1.Models[1].Model != "gpt-4" {
				t.Fatalf("row1 model order = %q, %q", row1.Models[0].Model, row1.Models[1].Model)
			}

			// gpt-4 model for zhangsan: 2 requests, 1 failed, totalTokens=300
			var m1 UsageReportModel
			for _, m := range row1.Models {
				if m.Model == "gpt-4" {
					m1 = m
					break
				}
			}
			if m1.Model == "" {
				t.Fatalf("row1 missing gpt-4 model")
			}
			if m1.TotalTokens != 300 || m1.Requests != 2 || m1.FailedRequests != 1 {
				t.Fatalf("row1 gpt-4 stats = %+v", m1)
			}
			if m1.CachedTokens != 10 || m1.TotalCacheTokens != 20 {
				t.Fatalf("row1 gpt-4 cache stats = %+v", m1)
			}
			// gpt-4: e-a1 has cached_tokens>0 (cache_hit=1), e-a2 has cached_tokens=0 (cache_hit=0)
			// cacheHitRate = 1 / 2 = 0.5
			if m1.CacheHitRate != 0.5 {
				t.Fatalf("row1 gpt-4 cacheHitRate = %f, want 0.5", m1.CacheHitRate)
			}

			// gpt-3.5-turbo model: 1 request, cached_tokens>0 (cache_hit=1)
			// cacheHitRate = 1 / 1 = 1.0
			var m2 UsageReportModel
			for _, m := range row1.Models {
				if m.Model == "gpt-3.5-turbo" {
					m2 = m
					break
				}
			}
			if m2.Model == "" {
				t.Fatalf("row1 missing gpt-3.5-turbo model")
			}
			if m2.TotalTokens != 50 || m2.Requests != 1 || m2.FailedRequests != 0 {
				t.Fatalf("row1 gpt-3.5-turbo stats = %+v", m2)
			}
			if m2.CachedTokens != 5 || m2.TotalCacheTokens != 10 {
				t.Fatalf("row1 gpt-3.5-turbo cache stats = %+v", m2)
			}
			if m2.CacheHitRate != 1.0 {
				t.Fatalf("row1 gpt-3.5-turbo cacheHitRate = %f, want 1.0", m2.CacheHitRate)
			}

			// Per-key aggregate totals for zhangsan: gpt-4 (300,2,1,10,20) + gpt-3.5-turbo (50,1,0,5,10)
			if row1.TotalTokens != 350 || row1.TotalRequests != 3 || row1.FailedRequests != 1 {
				t.Fatalf("row1 total aggregates = %+v", row1)
			}
			if row1.CachedTokens != 15 || row1.TotalCacheTokens != 30 {
				t.Fatalf("row1 cache aggregates = %+v", row1)
			}
			// Per-key cacheHitRate: totalCacheHits(2) / totalRequests(3)
			if row1.CacheHitRate != 2.0/3.0 {
				t.Fatalf("row1 cacheHitRate = %f, want %f", row1.CacheHitRate, 2.0/3.0)
			}
		})
	t.Run("empty range returns no results", func(t *testing.T) {
		rows, err := db.UsageReport(ctx, 1, 1)
		if err != nil {
			t.Fatalf("usage report empty range: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("rows = %d, want 0", len(rows))
		}
	})

	t.Run("events outside range are excluded", func(t *testing.T) {
		rows, err := db.UsageReport(ctx, 1_800_000_000_000, 1_900_000_000_000)
		if err != nil {
			t.Fatalf("usage report future range: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("rows = %d, want 0", len(rows))
		}
	})

}
