package usage

import (
	"regexp"
	"testing"
)

func TestKeyGenGenerateAPIKeyFormat(t *testing.T) {
	key, err := generateAPIKey("sh", "zs")
	if err != nil {
		t.Fatalf("generateAPIKey returned error: %v", err)
	}

	re := regexp.MustCompile(`^sh-zs-[A-Za-z0-9]{30}$`)
	if !re.MatchString(key) {
		t.Fatalf("key %q does not match expected format", key)
	}
}

func TestKeyGenPinyinAbbr(t *testing.T) {
	abbr, err := pinyinAbbr("张三丰")
	if err != nil {
		t.Fatalf("pinyinAbbr returned error: %v", err)
	}
	if abbr != "zsf" {
		t.Fatalf("expected zsf, got %s", abbr)
	}
}

func TestKeyGenCSVPreview(t *testing.T) {
	departments := []DepartmentLite{
		{ID: "dept-sh", Name: "上海总部", Prefix: "sh"},
		{ID: "dept-bj", Name: "北京分部", Prefix: "bj"},
	}

	csvContent := []byte("username,department\n张三,上海总部\n张三丰,北京分部\n王五,不存在部门\n")
	items, err := ParseCSVPreview(csvContent, departments)
	if err != nil {
		t.Fatalf("ParseCSVPreview returned error: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 preview items, got %d", len(items))
	}

	firstRE := regexp.MustCompile(`^sh-zs-[A-Za-z0-9]{30}$`)
	if items[0].Status != "ok" {
		t.Fatalf("row1 status expected ok, got %s", items[0].Status)
	}
	if !firstRE.MatchString(items[0].GeneratedKey) {
		t.Fatalf("row1 key %q does not match /^sh-zs-[A-Za-z0-9]{30}$/", items[0].GeneratedKey)
	}
	if items[0].Email != "" {
		t.Fatalf("row1 email expected empty (no email column in csv), got %q", items[0].Email)
	}

	secondRE := regexp.MustCompile(`^bj-zsf-[A-Za-z0-9]{30}$`)
	if items[1].Status != "ok" {
		t.Fatalf("row2 status expected ok, got %s", items[1].Status)
	}
	if !secondRE.MatchString(items[1].GeneratedKey) {
		t.Fatalf("row2 key %q does not match /^bj-zsf-[A-Za-z0-9]{30}$/", items[1].GeneratedKey)
	}
	if items[1].Email != "" {
		t.Fatalf("row2 email expected empty (no email column in csv), got %q", items[1].Email)
	}

	if items[2].Status != "error" {
		t.Fatalf("row3 status expected error for missing department, got %s", items[2].Status)
	}
	if items[2].Email != "" {
		t.Fatalf("row3 email expected empty (no email column in csv), got %q", items[2].Email)
	}
}

func TestKeyGenCSVPreviewChineseHeader(t *testing.T) {
	departments := []DepartmentLite{
		{ID: "dept-sh", Name: "上海总部", Prefix: "sh"},
	}

	csvContent := []byte("用户名,部门\n张三,上海总部\n")
	items, err := ParseCSVPreview(csvContent, departments)
	if err != nil {
		t.Fatalf("ParseCSVPreview returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 preview items, got %d", len(items))
	}
	if items[0].Status != "ok" {
		t.Fatalf("row1 status expected ok, got %s", items[0].Status)
	}
	if items[0].Email != "" {
		t.Fatalf("row1 email expected empty (no email column in csv), got %q", items[0].Email)
	}
}

func TestKeyGenCSVPreviewExportedHeader(t *testing.T) {
	departments := []DepartmentLite{
		{ID: "dept-sh", Name: "上海总部", Prefix: "sh"},
	}

	csvContent := []byte("用户名,API Key,部门\n\"\",\"sh-lisi-fk-001\",\"上海总部\"\n")
	items, err := ParseCSVPreview(csvContent, departments)
	if err != nil {
		t.Fatalf("ParseCSVPreview returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 preview items, got %d", len(items))
	}
	if items[0].Status != "ok" {
		t.Fatalf("row1 status expected ok, got %s", items[0].Status)
	}
	if items[0].GeneratedKey != "sh-lisi-fk-001" {
		t.Fatalf("row1 generated key expected sh-lisi-fk-001, got %s", items[0].GeneratedKey)
	}
	if items[0].Email != "" {
		t.Fatalf("row1 email expected empty (no email column in csv), got %q", items[0].Email)
	}
}

func TestKeyGenCSVPreviewChineseEmailHeader(t *testing.T) {
	departments := []DepartmentLite{{ID: "dept-sh", Name: "上海总部", Prefix: "sh"}}

	csvContent := []byte("用户名,邮箱,部门\n张三,zs@example.com,上海总部\n")
	items, err := ParseCSVPreview(csvContent, departments)
	if err != nil {
		t.Fatalf("ParseCSVPreview returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 preview item, got %d", len(items))
	}
	if items[0].Status != "ok" {
		t.Fatalf("row1 status expected ok, got %s", items[0].Status)
	}
	if items[0].Email != "zs@example.com" {
		t.Fatalf("row1 email = %q, want zs@example.com", items[0].Email)
	}
}

func TestKeyGenCSVPreviewEnglishEmailHeader(t *testing.T) {
	departments := []DepartmentLite{{ID: "dept-sh", Name: "上海总部", Prefix: "sh"}}

	csvContent := []byte("username,email,department\n张三,zs@example.com,上海总部\n")
	items, err := ParseCSVPreview(csvContent, departments)
	if err != nil {
		t.Fatalf("ParseCSVPreview returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 preview item, got %d", len(items))
	}
	if items[0].Status != "ok" {
		t.Fatalf("row1 status expected ok, got %s", items[0].Status)
	}
	if items[0].Email != "zs@example.com" {
		t.Fatalf("row1 email = %q, want zs@example.com", items[0].Email)
	}
}
