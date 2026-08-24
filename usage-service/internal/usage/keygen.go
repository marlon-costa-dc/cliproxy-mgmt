package usage

import (
	"crypto/rand"
	"encoding/csv"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"unicode"

	"github.com/mozillazg/go-pinyin"
)

// pinyinAbbr converts a Chinese name to lowercase pinyin initials.
func pinyinAbbr(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("name is empty")
	}

	args := pinyin.NewArgs()
	var b strings.Builder
	for _, r := range name {
		if !unicode.Is(unicode.Han, r) {
			continue
		}
		py := pinyin.Pinyin(string(r), args)
		if len(py) == 0 || len(py[0]) == 0 || py[0][0] == "" {
			continue
		}
		b.WriteByte(py[0][0][0])
	}

	abbr := strings.ToLower(b.String())
	if abbr == "" {
		return "", fmt.Errorf("no Chinese characters found in name: %s", name)
	}
	return abbr, nil
}

// generateAPIKey creates key in {prefix}-{abbr}-{30 alphanumeric chars} format.
func generateAPIKey(prefix, abbr string) (string, error) {
	prefix = strings.TrimSpace(strings.ToLower(prefix))
	abbr = strings.TrimSpace(strings.ToLower(abbr))
	if prefix == "" {
		return "", errors.New("prefix is empty")
	}
	if abbr == "" {
		return "", errors.New("abbr is empty")
	}

	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const randomLen = 30
	randPart := make([]byte, randomLen)
	limit := big.NewInt(int64(len(charset)))

	for i := 0; i < randomLen; i++ {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("generate random: %w", err)
		}
		randPart[i] = charset[n.Int64()]
	}

	return fmt.Sprintf("%s-%s-%s", prefix, abbr, string(randPart)), nil
}

type DepartmentLite struct {
	ID     string
	Name   string
	Prefix string
}

type KeyGenPreviewItem struct {
	UserName       string `json:"userName"`
	DepartmentName string `json:"departmentName"`
	DepartmentID   string `json:"departmentId"`
	Email          string `json:"email,omitempty"`
	GeneratedKey   string `json:"generatedKey,omitempty"`
	Status         string `json:"status"` // ok | error | warning
	ErrorReason    string `json:"errorReason,omitempty"`
}

func GenerateAPIKeyForUser(prefix, userName string) (string, error) {
	abbr, err := pinyinAbbr(userName)
	if err != nil {
		return "", err
	}
	return generateAPIKey(prefix, abbr)
}

// ParseCSVPreview parses CSV and generates preview rows.
func ParseCSVPreview(csvContent []byte, departments []DepartmentLite) ([]KeyGenPreviewItem, error) {
	reader := csv.NewReader(strings.NewReader(string(csvContent)))
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse csv: %w", err)
	}
	if len(records) == 0 {
		return []KeyGenPreviewItem{}, nil
	}

	header := records[0]
	if len(header) < 2 {
		return nil, errors.New("invalid csv header: expected username,department")
	}
	normalizeHeader := func(v string) string {
		v = strings.TrimPrefix(v, "\ufeff")
		v = strings.TrimSpace(strings.ToLower(v))
		v = strings.ReplaceAll(v, "_", "")
		v = strings.ReplaceAll(v, " ", "")
		return v
	}
	h := make([]string, 0, len(header))
	for _, col := range header {
		h = append(h, normalizeHeader(col))
	}
	usernameIdx := -1
	departmentIdx := -1
	apiKeyIdx := -1
	emailIdx := -1
	for i, col := range h {
		if col == "username" || col == "用户名" {
			usernameIdx = i
		}
		if col == "department" || col == "部门" {
			departmentIdx = i
		}
		if col == "apikey" || col == "api密钥" || col == "api秘钥" {
			apiKeyIdx = i
		}
		if col == "email" || col == "e-mail" || col == "邮箱" || col == "邮件" {
			emailIdx = i
		}
	}
	if usernameIdx < 0 || departmentIdx < 0 {
		return nil, errors.New("invalid csv header: expected username,department")
	}

	deptByName := make(map[string]DepartmentLite, len(departments))
	for _, d := range departments {
		deptByName[strings.TrimSpace(d.Name)] = d
	}

	items := make([]KeyGenPreviewItem, 0, len(records)-1)
	for i := 1; i < len(records); i++ {
		row := records[i]
		maxIdx := usernameIdx
		if departmentIdx > maxIdx {
			maxIdx = departmentIdx
		}
		if apiKeyIdx > maxIdx {
			maxIdx = apiKeyIdx
		}
		if emailIdx > maxIdx {
			maxIdx = emailIdx
		}
		if len(row) <= maxIdx {
			items = append(items, KeyGenPreviewItem{Status: "error", ErrorReason: fmt.Sprintf("row %d: expected username,department", i+1)})
			continue
		}

		userName := strings.TrimSpace(row[usernameIdx])
		deptName := strings.TrimSpace(row[departmentIdx])
		existingKey := ""
		if apiKeyIdx >= 0 && len(row) > apiKeyIdx {
			existingKey = strings.TrimSpace(row[apiKeyIdx])
		}
		email := ""
		if emailIdx >= 0 && len(row) > emailIdx {
			email = strings.TrimSpace(row[emailIdx])
		}
		if deptName == "" || (userName == "" && existingKey == "") {
			items = append(items, KeyGenPreviewItem{
				UserName:       userName,
				DepartmentName: deptName,
				Email:          email,
				Status:         "error",
				ErrorReason:    "username and department are required",
			})
			continue
		}

		dept, ok := deptByName[deptName]
		if !ok {
			items = append(items, KeyGenPreviewItem{
				UserName:       userName,
				DepartmentName: deptName,
				Email:          email,
				Status:         "error",
				ErrorReason:    fmt.Sprintf("department not found: %s", deptName),
			})
			continue
		}

		key := existingKey
		if key == "" {
			abbr, err := pinyinAbbr(userName)
			if err != nil {
				items = append(items, KeyGenPreviewItem{
					UserName:       userName,
					DepartmentName: deptName,
					DepartmentID:   dept.ID,
					Email:          email,
					Status:         "error",
					ErrorReason:    err.Error(),
				})
				continue
			}

			key, err = generateAPIKey(dept.Prefix, abbr)
			if err != nil {
				items = append(items, KeyGenPreviewItem{
					UserName:       userName,
					DepartmentName: deptName,
					DepartmentID:   dept.ID,
					Email:          email,
					Status:         "error",
					ErrorReason:    err.Error(),
				})
				continue
			}
		}

		items = append(items, KeyGenPreviewItem{
			UserName:       userName,
			DepartmentName: deptName,
			DepartmentID:   dept.ID,
			Email:          email,
			GeneratedKey:   key,
			Status:         "ok",
		})
	}

	return items, nil
}
