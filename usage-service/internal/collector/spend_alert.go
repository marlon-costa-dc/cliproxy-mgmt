package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/mail"
	"github.com/seakee/cpa-manager/usage-service/internal/store"
)

// CheckUserSpendAlerts queries all users' daily spend and sends emails for any
// new threshold milestones crossed ($ThresholdCents, 2x, 3x, ...).
func CheckUserSpendAlerts(s *store.Store, sender *mail.Sender, thresholdCents int64) {
	if sender == nil {
		log.Printf("spend-alert: sender is nil, skipping check")
		return
	}
	if thresholdCents <= 0 {
		log.Printf("spend-alert: thresholdCents=%d <= 0, skipping check", thresholdCents)
		return
	}

	log.Printf("spend-alert: starting check, thresholdCents=%d", thresholdCents)

	ctx := context.Background()

	users, err := s.QueryUserSpend(ctx)
	if err != nil {
		log.Printf("spend-alert: QueryUserSpend failed: %v", err)
		return
	}
	if len(users) == 0 {
		log.Println("spend-alert: no users with spend found")
		return
	}
	log.Printf("spend-alert: found %d users with spend", len(users))

	notified, err := s.LoadUserAlertThresholds(ctx)
	if err != nil {
		log.Printf("spend-alert: LoadUserAlertThresholds failed: %v", err)
		return
	}
	log.Printf("spend-alert: LoadUserAlertThresholds returned, have %d user records", len(notified))

	// Load latest pool quota summary for inclusion in email
	poolSummary, _ := s.LoadPoolQuotaSummary(ctx)
	var poolInfo *mail.PoolInfo
	if poolSummary != nil {
		poolInfo = &mail.PoolInfo{
			TotalAccounts:   poolSummary.TotalEnabled,
			Exhausted5h:     poolSummary.FiveHourExhausted,
			ExhaustedWeekly: poolSummary.WeeklyExhausted,
			AvgUsed5h:       poolSummary.FiveHourAvgUsed,
			AvgUsedWeekly:   poolSummary.WeeklyAvgUsed,
			EarliestReset:   poolSummary.EarliestReset,
		}
		if len(poolSummary.Accounts) > 0 {
			poolInfo.Accounts = make([]mail.QuotaInfo, len(poolSummary.Accounts))
			for i, a := range poolSummary.Accounts {
				poolInfo.Accounts[i] = mail.QuotaInfo{
					Account:       a.Account,
					FiveHourUsed:  a.FiveHourUsed,
					FiveHourReset: a.FiveHourReset,
					WeeklyUsed:    a.WeeklyUsed,
					WeeklyReset:   a.WeeklyReset,
					Error:         a.Error,
				}
			}
		}
	}

	for _, u := range users {
		if u.Email == "" {
			log.Printf("spend-alert: user %s has no email, skipping", u.UserName)
			continue
		}
		maxMultiple := u.TodayCents / thresholdCents
		log.Printf("spend-alert: user %s todayCents=%d, maxMultiple=%d (thresholdCents=%d)", u.UserName, u.TodayCents, maxMultiple, thresholdCents)
		if maxMultiple == 0 {
			continue
		}

		userNotified := notified[u.UserName]
		log.Printf("spend-alert: user %s has %d already-notified thresholds", u.UserName, len(userNotified))
		for multiple := int64(1); multiple <= maxMultiple; multiple++ {
			threshold := multiple * thresholdCents
			if userNotified[threshold] {
				log.Printf("spend-alert: user %s threshold $%d already notified today, skip", u.UserName, threshold/100)
				continue
			}

			thresholdDollars := threshold / 100
			subject := "API 额度使用提醒"
			body := mail.BuildAlertBody(u.UserName, u.TodayCents, thresholdDollars, thresholdCents, poolInfo)

			if err := sender.Send(u.Email, subject, body); err != nil {
				log.Printf("spend-alert: failed to send alert to %s (%s): %v",
					u.UserName, u.Email, err)
				continue
			}
			log.Printf("spend-alert: sent $%d alert to %s <%s> (today=$%d)",
				thresholdDollars, u.UserName, u.Email, u.TodayCents)

			if err := s.RecordUserAlert(ctx, u.UserName, threshold); err != nil {
				log.Printf("spend-alert: RecordUserAlert failed for %s threshold=%d: %v",
					u.UserName, threshold, err)
			}
		}
	}
}

// accountQuota holds Codex quota data for a single account fetched via CPA proxy.
type accountQuota struct {
	account       string
	fiveHourUsed  float64
	fiveHourReset string
	weeklyUsed    float64
	weeklyReset   string
	err           string
}

// poolQuotaChecker checks whether all accounts in the pool have exhausted their
// 5-hour or weekly Codex quota windows and sends notification emails.
type poolQuotaChecker struct {
	store       *store.Store
	sender      *mail.Sender
	cpaBaseURL  string
	mgmtKey     string
}

func newPoolQuotaChecker(s *store.Store, sender *mail.Sender, cpaBaseURL, mgmtKey string) *poolQuotaChecker {
	return &poolQuotaChecker{
		store:      s,
		sender:     sender,
		cpaBaseURL: strings.TrimRight(cpaBaseURL, "/"),
		mgmtKey:    mgmtKey,
	}
}

const (
	fiveHourWindowSeconds = 18_000
	weeklyWindowSeconds   = 604_800
)

// codexUsageResponse mirrors the Codex /backend-api/wham/usage response shape.
type codexUsageResponse struct {
	RateLimit    *codexRateLimit `json:"rate_limit"`
	RateLimitAlt *codexRateLimit `json:"rateLimit"`
}

type codexRateLimit struct {
	PrimaryWindow    *codexWindow `json:"primary_window"`
	PrimaryWindowAlt *codexWindow `json:"primaryWindow"`
	SecondaryWindow    *codexWindow `json:"secondary_window"`
	SecondaryWindowAlt *codexWindow `json:"secondaryWindow"`
}

type codexWindow struct {
	UsedPercent    *float64 `json:"used_percent"`
	UsedPercentAlt *float64 `json:"usedPercent"`
	LimitSeconds    *float64 `json:"limit_window_seconds"`
	LimitSecondsAlt *float64 `json:"limitWindowSeconds"`
	ResetAt    *float64 `json:"reset_at"`
	ResetAtAlt *float64 `json:"resetAt"`
}

func (r codexUsageResponse) rateLimit() *codexRateLimit {
	if r.RateLimit != nil {
		return r.RateLimit
	}
	return r.RateLimitAlt
}

func (r *codexRateLimit) classifyWindows() (*codexWindow, *codexWindow) {
	if r == nil {
		return nil, nil
	}
	primaryWindow := firstCodexWindow(r.PrimaryWindow, r.PrimaryWindowAlt)
	secondaryWindow := firstCodexWindow(r.SecondaryWindow, r.SecondaryWindowAlt)
	windows := []*codexWindow{primaryWindow, secondaryWindow}

	var fiveHourWindow *codexWindow
	var weeklyWindow *codexWindow
	for _, window := range windows {
		if window == nil {
			continue
		}
		seconds, ok := window.limitWindowSeconds()
		if !ok {
			continue
		}
		if seconds == fiveHourWindowSeconds && fiveHourWindow == nil {
			fiveHourWindow = window
			continue
		}
		if seconds == weeklyWindowSeconds && weeklyWindow == nil {
			weeklyWindow = window
		}
	}

	if fiveHourWindow == nil && primaryWindow != nil && primaryWindow != weeklyWindow {
		fiveHourWindow = primaryWindow
	}
	if weeklyWindow == nil && secondaryWindow != nil && secondaryWindow != fiveHourWindow {
		weeklyWindow = secondaryWindow
	}
	return fiveHourWindow, weeklyWindow
}

func firstCodexWindow(windows ...*codexWindow) *codexWindow {
	for _, window := range windows {
		if window != nil {
			return window
		}
	}
	return nil
}

func (w *codexWindow) usedPercent() (float64, bool) {
	if w == nil {
		return 0, false
	}
	if w.UsedPercent != nil {
		return *w.UsedPercent, true
	}
	if w.UsedPercentAlt != nil {
		return *w.UsedPercentAlt, true
	}
	return 0, false
}

func (w *codexWindow) limitWindowSeconds() (int64, bool) {
	if w == nil {
		return 0, false
	}
	if w.LimitSeconds != nil {
		return int64(*w.LimitSeconds), true
	}
	if w.LimitSecondsAlt != nil {
		return int64(*w.LimitSecondsAlt), true
	}
	return 0, false
}

func (w *codexWindow) resetAt() (int64, bool) {
	if w == nil {
		return 0, false
	}
	if w.ResetAt != nil {
		return int64(*w.ResetAt), true
	}
	if w.ResetAtAlt != nil {
		return int64(*w.ResetAtAlt), true
	}
	return 0, false
}

// loadEnabledAuthIndices fetches auth files from CPA and returns a map of
// auth_index → account_id (chatgpt account id for codex api-call header).
// Only enabled accounts are included. Returns nil on error.
func (c *poolQuotaChecker) loadEnabledAuthIndices(ctx context.Context) map[string]string {
	apiURL := fmt.Sprintf("%s/v0/management/auth-files", c.cpaBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		log.Printf("pool-quota: create auth-files request: %v", err)
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+c.mgmtKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("pool-quota: fetch auth-files failed: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("pool-quota: auth-files status %d", resp.StatusCode)
		return nil
	}

	var payload struct {
		Files []map[string]any `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		log.Printf("pool-quota: decode auth-files: %v", err)
		return nil
	}

	enabled := make(map[string]string, len(payload.Files))
	for _, f := range payload.Files {
		authIndex := readMapString(f, "auth_index", "authIndex", "auth-index")
		if authIndex == "" {
			continue
		}
		if isDisabled(f["disabled"]) {
			log.Printf("pool-quota: skipping disabled account %s", authIndex)
			continue
		}
		accountID := resolveAccountID(f)
		enabled[authIndex] = accountID
	}
	return enabled
}

// resolveAccountID extracts the Codex account id from an auth file map.
// The id may be at the top level, nested in metadata/attributes, or encoded
// in an id_token JWT. Returns empty string if not found.
func resolveAccountID(f map[string]any) string {
	// Direct fields at top level
	if id := readMapString(f, "chatgpt_account_id", "chatgptAccountId",
		"account_id", "accountId", "account"); id != "" {
		return id
	}
	// Nested in metadata
	if m, ok := f["metadata"].(map[string]any); ok {
		if id := readMapString(m, "chatgpt_account_id", "chatgptAccountId",
			"account_id", "accountId"); id != "" {
			return id
		}
	}
	// Nested in attributes
	if a, ok := f["attributes"].(map[string]any); ok {
		if id := readMapString(a, "chatgpt_account_id", "chatgptAccountId",
			"account_id", "accountId"); id != "" {
			return id
		}
	}
	return ""
}

// readMapString reads the first non-empty string value from a map by the given keys.
func readMapString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		v, ok := m[key]
		if !ok || v == nil {
			continue
		}
		switch s := v.(type) {
		case string:
			if s = strings.TrimSpace(s); s != "" {
				return s
			}
		}
	}
	return ""
}

// isDisabled returns true if the auth file's disabled field indicates a disabled account.
func isDisabled(d any) bool {
	if d == nil {
		return false
	}
	switch v := d.(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1"
	case float64:
		return v != 0
	default:
		return false
	}
}

// isPermanentQuotaError returns true if the error indicates the upstream account
// itself is invalid (the Codex API rejected the account's key with 401/403).
// Network-level failures (timeout, 502, 503, connection refused, etc.) are NOT
// permanent — they are retried with backoff.
func isPermanentQuotaError(errMsg string) bool {
	// "upstream status" means Codex (chatgpt.com) rejected the request.
	// 401/403 means the account's API key/session is invalid — retrying won't help.
	return strings.Contains(errMsg, "upstream status 401") ||
		strings.Contains(errMsg, "upstream status 403")
}

// CheckPoolQuota runs one round of pool quota exhaustion detection.
func (c *poolQuotaChecker) Check(ctx context.Context) {
	if c.sender == nil || c.cpaBaseURL == "" || c.mgmtKey == "" {
		return
	}

	// 1. Get distinct auth_indices from recent successful usage_events
	authIndices, err := c.store.LoadDistinctAuthIndices(ctx)
	if err != nil {
		log.Printf("pool-quota: LoadDistinctAuthIndices failed: %v", err)
		return
	}
	if len(authIndices) == 0 {
		return
	}

	// 1b. Filter to only enabled accounts (skip disabled ones)
	// accountIDs maps auth_index → chatgpt account id (for api-call header)
	var accountIDs map[string]string
	authMeta := c.loadEnabledAuthIndices(ctx)
	if authMeta != nil {
		accountIDs = authMeta
		filtered := make([]string, 0, len(authIndices))
		for _, ai := range authIndices {
			if _, ok := authMeta[ai]; ok {
				filtered = append(filtered, ai)
			} else {
				log.Printf("pool-quota: skipping auth_index %s (not in enabled set)", ai)
			}
		}
		authIndices = filtered
		if len(authIndices) == 0 {
			log.Println("pool-quota: no enabled accounts to check")
			return
		}
	} else {
		log.Println("pool-quota: auth-files unavailable, falling back to all auth indices from usage_events")
	}

	// Log resolved account IDs for debugging
	if accountIDs != nil {
		for _, ai := range authIndices {
			if actID := accountIDs[ai]; actID != "" {
				log.Printf("pool-quota: auth_index %s → accountID %s", ai, actID)
			} else {
				log.Printf("pool-quota: auth_index %s → no accountID found in auth-file", ai)
			}
		}
	}

	// Rate limit: only check up to 50 accounts per run
	if len(authIndices) > 50 {
		log.Printf("pool-quota: %d auth indices exceeds limit 50, skipping check to avoid false positives", len(authIndices))
		return
	}

	// 2. For each auth_index, query Codex quota via CPA proxy API (parallel)
	type accountResult struct {
		quota accountQuota
	}

	queryAccounts := func(indices []string) []accountQuota {
		if len(indices) == 0 {
			return nil
		}
		ch := make(chan accountResult, len(indices))
		bgCtx := context.Background()
		for _, ai := range indices {
			ai := ai
			actID := accountIDs[ai]
			go func() {
				ch <- accountResult{quota: c.queryAccountQuota(bgCtx, ai, actID)}
			}()
		}
		results := make([]accountQuota, 0, len(indices))
		for range indices {
			r := <-ch
			results = append(results, r.quota)
		}
		return results
	}

	results := queryAccounts(authIndices)

	// 2b. Log per-account result and classify for retry
	for _, r := range results {
		if r.err == "" {
			log.Printf("pool-quota: account %s OK (5h=%.0f%% weekly=%.0f%%)",
				r.account, r.fiveHourUsed, r.weeklyUsed)
			continue
		}
		if isPermanentQuotaError(r.err) {
			log.Printf("pool-quota: account %s permanently unavailable (API Key invalid), skip retry: %s", r.account, r.err)
		} else {
			log.Printf("pool-quota: account %s transient error, will retry: %s", r.account, r.err)
		}
	}

	const maxRetries = 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		var retryIndices []string
		for _, r := range results {
			if r.err != "" && !isPermanentQuotaError(r.err) {
				retryIndices = append(retryIndices, r.account)
			}
		}
		if len(retryIndices) == 0 {
			break
		}
		backoff := time.Duration(500<<attempt) * time.Millisecond // 500ms, 1s, 2s
		log.Printf("pool-quota: retrying %d accounts (attempt %d/%d, backoff %v)",
			len(retryIndices), attempt+1, maxRetries, backoff)
		time.Sleep(backoff)

		retryResults := queryAccounts(retryIndices)
		// Merge retry results into original results
		retryMap := make(map[string]accountQuota, len(retryResults))
		for _, r := range retryResults {
			retryMap[r.account] = r
		}
		for i, r := range results {
			if newR, ok := retryMap[r.account]; ok {
				results[i] = newR
			}
		}
	}

	// 2c. After retry, if any accounts still have transient errors, skip alert
	var transientAfterRetry int
	for _, r := range results {
		if r.err != "" && !isPermanentQuotaError(r.err) {
			transientAfterRetry++
		}
	}
	if transientAfterRetry > 0 {
		log.Printf("pool-quota: %d accounts still in transient error state after %d retries, cannot determine pool state, skipping check",
			transientAfterRetry, maxRetries)
		return
	}

	// 3. Aggregate: find if ALL (non-permanently-failed) accounts have 5-hour or weekly at >= 100%
	var total5h, exhausted5h int
	var totalWeek, exhaustedWeek int
	var earliest5hReset, earliestWeekReset string
	var any5hSuccess, anyWeekSuccess bool
	var sum5hUsed, sumWeekUsed float64

	for _, r := range results {
		if r.err != "" {
			// Only permanent errors remain here (transient ones caused early return above)
			log.Printf("pool-quota: account %s excluded from pool check (API Key invalid): %s", r.account, r.err)
			continue
		}
		// 5-hour
		if r.fiveHourUsed >= 0 {
			total5h++
			any5hSuccess = true
			sum5hUsed += r.fiveHourUsed
			if r.fiveHourUsed >= 100 {
				exhausted5h++
			}
			if earliest5hReset == "" || r.fiveHourReset < earliest5hReset {
				earliest5hReset = r.fiveHourReset
			}
		}
		// Weekly
		if r.weeklyUsed >= 0 {
			totalWeek++
			anyWeekSuccess = true
			sumWeekUsed += r.weeklyUsed
			if r.weeklyUsed >= 100 {
				exhaustedWeek++
			}
			if earliestWeekReset == "" || r.weeklyReset < earliestWeekReset {
				earliestWeekReset = r.weeklyReset
			}
		}
	}

	// Compute averages
	var avg5hUsed, avgWeekUsed float64
	if total5h > 0 {
		avg5hUsed = sum5hUsed / float64(total5h)
	}
	if totalWeek > 0 {
		avgWeekUsed = sumWeekUsed / float64(totalWeek)
	}

	// Save pool quota summary for inclusion in spend alert emails
	// Only include accounts that returned valid quota data
	accountsData := make([]store.AccountData, 0, len(results))
	for _, r := range results {
		if r.err != "" {
			continue // skip permanently unavailable accounts
		}
		ad := store.AccountData{
			Account:       r.account,
			FiveHourUsed:  r.fiveHourUsed,
			FiveHourReset: r.fiveHourReset,
			WeeklyUsed:    r.weeklyUsed,
			WeeklyReset:   r.weeklyReset,
		}
		accountsData = append(accountsData, ad)
	}
	if err := c.store.SavePoolQuotaSummary(ctx, store.PoolQuotaSummary{
		TotalEnabled:      len(accountsData),
		FiveHourExhausted: exhausted5h,
		WeeklyExhausted:   exhaustedWeek,
		FiveHourAvgUsed:   avg5hUsed,
		WeeklyAvgUsed:     avgWeekUsed,
		EarliestReset:     pickEarliestNonEmpty(earliest5hReset, earliestWeekReset),
		UpdatedAtMS:       time.Now().UnixMilli(),
		Accounts:          accountsData,
	}); err != nil {
		log.Printf("pool-quota: SavePoolQuotaSummary failed: %v", err)
	}

	// 4. Send alerts if all accounts exhausted
	allEmails, err := c.store.LoadAllUserEmails(ctx)
	if err != nil {
		log.Printf("pool-quota: LoadAllUserEmails failed: %v", err)
		return
	}
	if len(allEmails) == 0 {
		return
	}
	// Build recipient list (deduplicate)
	emailSet := make(map[string]string)
	for _, email := range allEmails {
		if email != "" {
			emailSet[email] = email
		}
	}

	// Build account detail info for email
	accountInfos := make([]mail.AccountQuotaInfo, 0, len(results))
	for _, r := range results {
		info := mail.AccountQuotaInfo{
			Account:      r.account,
			FiveHourUsed: r.fiveHourUsed,
			FiveHourReset: r.fiveHourReset,
			WeeklyUsed:   r.weeklyUsed,
			WeeklyReset:  r.weeklyReset,
		}
		if r.err != "" {
			info.Error = r.err
		}
		accountInfos = append(accountInfos, info)
	}

	c.notifyIfExhausted(ctx, "five_hour", any5hSuccess, total5h, exhausted5h, earliest5hReset, emailSet, accountInfos)
	c.notifyIfExhausted(ctx, "weekly", anyWeekSuccess, totalWeek, exhaustedWeek, earliestWeekReset, emailSet, accountInfos)
}

func (c *poolQuotaChecker) notifyIfExhausted(ctx context.Context, windowType string, anySuccess bool, total, exhausted int, earliestReset string, emails map[string]string, accountInfos []mail.AccountQuotaInfo) {
	if !anySuccess || total == 0 || exhausted < total {
		// Clear pool quota alert so next full exhaustion re-triggers
		existing, _ := c.store.LoadPoolQuotaAlert(ctx, windowType)
		if existing != nil {
			if err := c.store.UpsertPoolQuotaAlert(ctx, windowType, 0, 0); err != nil {
				log.Printf("pool-quota: clear PoolQuotaAlert(%s) failed: %v", windowType, err)
			}
		}
		return
	}

	// Check if already notified
	existing, err := c.store.LoadPoolQuotaAlert(ctx, windowType)
	if err != nil {
		log.Printf("pool-quota: LoadPoolQuotaAlert(%s) failed: %v", windowType, err)
		return
	}
	if existing != nil && existing.NotifiedAtMS > 0 {
		// Already notified — skip, but re-notify after reset clears the record
		return
	}

	if earliestReset == "" {
		earliestReset = "未知"
	}

	now := time.Now().UnixMilli()
	subject := "API 服务额度耗尽通知"
	body := mail.BuildPoolExhaustedBody(windowType, earliestReset, accountInfos)

	for _, email := range emails {
		if err := c.sender.Send(email, subject, body); err != nil {
			log.Printf("pool-quota: send to %s failed: %v", email, err)
			continue
		}
		log.Printf("pool-quota: sent %s exhaustion alert to %s", windowType, email)
	}

	if err := c.store.UpsertPoolQuotaAlert(ctx, windowType, now, now); err != nil {
		log.Printf("pool-quota: UpsertPoolQuotaAlert(%s) failed: %v", windowType, err)
	}
}

// queryAccountQuota calls CPA proxy to query Codex usage for a single auth_index.
func (c *poolQuotaChecker) queryAccountQuota(ctx context.Context, authIndex, accountID string) accountQuota {
	result := accountQuota{account: authIndex, fiveHourUsed: -1, weeklyUsed: -1}

	// Build the CPA api-call request.
	// The "header" field with "Bearer $TOKEN$" tells CPA to inject the
	// actual auth token from the account's auth file. Without this header,
	// CPA forwards an unauthenticated request and Codex returns 401.
	apiCallPayload := map[string]any{
		"auth_index": authIndex,
		"method":     "GET",
		"url":        "https://chatgpt.com/backend-api/wham/usage",
	}
	headers := map[string]string{
		"Authorization": "Bearer $TOKEN$",
		"Content-Type":  "application/json",
		"User-Agent":    "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal",
	}
	if accountID != "" {
		headers["Chatgpt-Account-Id"] = accountID
	}
	apiCallPayload["header"] = headers
	body, _ := json.Marshal(apiCallPayload)

	apiURL := fmt.Sprintf("%s/v0/management/api-call", c.cpaBaseURL)
	reqCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, apiURL, strings.NewReader(string(body)))
	if err != nil {
		result.err = fmt.Sprintf("network: create request: %v", err)
		return result
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.mgmtKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		result.err = fmt.Sprintf("network: %v", err)
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.err = fmt.Sprintf("proxy status %d", resp.StatusCode)
		return result
	}

	// CPA api-call wraps the response: { status_code, header, body }
	var apiResp struct {
		StatusCode int             `json:"status_code"`
		Body       json.RawMessage `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		result.err = fmt.Sprintf("network: decode api-call response: %v", err)
		return result
	}

	if apiResp.StatusCode < 200 || apiResp.StatusCode >= 300 {
		result.err = fmt.Sprintf("upstream status %d", apiResp.StatusCode)
		return result
	}

	// Parse the Codex usage response.
	// The CPA proxy wraps the upstream body as a JSON string (double-encoded),
	// so we must unwrap it first if it is a quoted string.
	raw := apiResp.Body
	var bodyStr string
	if json.Unmarshal(raw, &bodyStr) == nil {
		raw = []byte(bodyStr)
	}
	var usage codexUsageResponse
	if err := json.Unmarshal(raw, &usage); err != nil {
		result.err = fmt.Sprintf("network: parse codex usage: %v", err)
		return result
	}

	rl := usage.rateLimit()
	if rl == nil {
		result.err = "network: no rate_limit in response"
		return result
	}

	fiveHourWindow, weeklyWindow := rl.classifyWindows()
	if usedPercent, ok := fiveHourWindow.usedPercent(); ok {
		result.fiveHourUsed = usedPercent
	}
	if resetAt, ok := fiveHourWindow.resetAt(); ok {
		result.fiveHourReset = epochToTimeStr(resetAt)
	}
	if usedPercent, ok := weeklyWindow.usedPercent(); ok {
		result.weeklyUsed = usedPercent
	}
	if resetAt, ok := weeklyWindow.resetAt(); ok {
		result.weeklyReset = epochToTimeStr(resetAt)
	}

	return result
}

func epochToTimeStr(epochSec int64) string {
	t := time.Unix(epochSec, 0)
	return t.Format("2006-01-02 15:04 MST")
}

// pickEarliestNonEmpty returns the earliest (smallest) non-empty string.
func pickEarliestNonEmpty(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	if a < b {
		return a
	}
	return b
}
