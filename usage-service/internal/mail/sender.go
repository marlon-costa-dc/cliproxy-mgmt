package mail

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strings"
	"time"
)

type Config struct {
	Host                string
	Port                int
	Username            string
	Password            string
	From                string
	FromName            string
	Secure              bool   // true = implicit TLS (port 465)
	InsecureSkipVerify  bool   // skip TLS cert verification
}

type Sender struct {
	cfg Config
}

func NewSender(cfg Config) *Sender {
	return &Sender{cfg: cfg}
}

func (s *Sender) Send(to, subject, htmlBody string) error {
	if s.cfg.Host == "" || s.cfg.Port == 0 {
		return fmt.Errorf("mail: SMTP not configured")
	}
	fromName := s.cfg.FromName
	if fromName == "" {
		fromName = "API Management"
	}
	from := s.cfg.From
	if from == "" {
		from = s.cfg.Username
	}

	header := fmt.Sprintf("From: %s <%s>\r\n", fromName, from)
	header += fmt.Sprintf("To: %s\r\n", to)
	header += fmt.Sprintf("Subject: %s\r\n", subject)
	header += "MIME-Version: 1.0\r\n"
	header += "Content-Type: text/html; charset=UTF-8\r\n"
	header += "\r\n"

	msg := header + htmlBody
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)

	// Port 465 always uses implicit TLS; otherwise follow the Secure flag
	useImplicitTLS := s.cfg.Port == 465 || s.cfg.Secure

	if useImplicitTLS {
		return s.sendImplicitTLS(addr, from, to, []byte(msg))
	}

	// Try STARTTLS first; fall back to plain AUTH LOGIN if TLS handshake fails.
	err := s.sendSTARTTLS(addr, from, to, []byte(msg))
	if err != nil && isTLSHandshakeError(err) {
		log.Printf("mail: STARTTLS handshake failed (%v), falling back to plain AUTH LOGIN", err)
		return s.sendPlainAuth(addr, from, to, []byte(msg))
	}
	return err
}

// sendImplicitTLS dials a raw TLS connection (for SMTPS on port 465).
func (s *Sender) sendImplicitTLS(addr, from, to string, msg []byte) error {
	tlsCfg := &tls.Config{
		ServerName:         s.cfg.Host,
		InsecureSkipVerify: s.cfg.InsecureSkipVerify,
	}
	// Internal SMTP servers (port 465) typically use self-signed certs
	// and RSA-only cipher suites not in Go's default set.
	if s.cfg.Port == 465 {
		tlsCfg.InsecureSkipVerify = true
		tlsCfg.MinVersion = tls.VersionTLS10
		tlsCfg.MaxVersion = tls.VersionTLS12
		tlsCfg.CipherSuites = []uint16{
			tls.TLS_RSA_WITH_AES_128_CBC_SHA,
			tls.TLS_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		}
	}
	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("tls dial: %w", err)
	}

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	if s.cfg.Username != "" || s.cfg.Password != "" {
		auth := &plainNoTLS{smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)}
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}

	if err := client.Mail(from); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return client.Quit()
}

// plainNoTLS wraps smtp.PlainAuth but tells it the connection is TLS-secured.
type plainNoTLS struct {
	smtp.Auth
}

func (a *plainNoTLS) Start(server *smtp.ServerInfo) (string, []byte, error) {
	server.TLS = true
	return a.Auth.Start(server)
}

// sendPlainAuth sends email via plain-text AUTH LOGIN.
func (s *Sender) sendPlainAuth(addr, from, to string, msg []byte) error {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(30 * time.Second))

	readLine := func() (string, error) {
		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			return "", err
		}
		return string(buf[:n]), nil
	}
	writeCmd := func(cmd string) error {
		_, err := conn.Write([]byte(cmd + "\r\n"))
		return err
	}

	if _, err := readLine(); err != nil {
		return fmt.Errorf("banner: %w", err)
	}
	if err := writeCmd("EHLO mail"); err != nil {
		return fmt.Errorf("ehlo: %w", err)
	}
	if _, err := readLine(); err != nil {
		return fmt.Errorf("ehlo response: %w", err)
	}
	if err := writeCmd("AUTH LOGIN"); err != nil {
		return fmt.Errorf("auth login cmd: %w", err)
	}
	resp, err := readLine()
	if err != nil {
		return fmt.Errorf("auth login response: %w", err)
	}
	if !strings.HasPrefix(resp, "334") {
		return fmt.Errorf("auth login not supported: %s", resp)
	}
	if err := writeCmd(base64.StdEncoding.EncodeToString([]byte(s.cfg.Username))); err != nil {
		return fmt.Errorf("auth user: %w", err)
	}
	resp, err = readLine()
	if err != nil {
		return fmt.Errorf("auth user response: %w", err)
	}
	if !strings.HasPrefix(resp, "334") {
		return fmt.Errorf("auth user rejected: %s", resp)
	}
	if err := writeCmd(base64.StdEncoding.EncodeToString([]byte(s.cfg.Password))); err != nil {
		return fmt.Errorf("auth pass: %w", err)
	}
	resp, err = readLine()
	if err != nil {
		return fmt.Errorf("auth pass response: %w", err)
	}
	if !strings.HasPrefix(resp, "235") {
		return fmt.Errorf("auth failed: %s", resp)
	}
	if err := writeCmd(fmt.Sprintf("MAIL FROM:<%s>", from)); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if _, err := readLine(); err != nil {
		return fmt.Errorf("mail from response: %w", err)
	}
	if err := writeCmd(fmt.Sprintf("RCPT TO:<%s>", to)); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}
	if _, err := readLine(); err != nil {
		return fmt.Errorf("rcpt to response: %w", err)
	}
	if err := writeCmd("DATA"); err != nil {
		return fmt.Errorf("data cmd: %w", err)
	}
	resp, err = readLine()
	if err != nil {
		return fmt.Errorf("data response: %w", err)
	}
	if !strings.HasPrefix(resp, "354") {
		return fmt.Errorf("data rejected: %s", resp)
	}
	if _, err := conn.Write(msg); err != nil {
		return fmt.Errorf("write body: %w", err)
	}
	if _, err := conn.Write([]byte("\r\n.\r\n")); err != nil {
		return fmt.Errorf("write end: %w", err)
	}
	resp, err = readLine()
	if err != nil {
		return fmt.Errorf("data end response: %w", err)
	}
	if !strings.HasPrefix(resp, "250") {
		return fmt.Errorf("send failed: %s", resp)
	}
	writeCmd("QUIT")
	return nil
}

func isTLSHandshakeError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "tls: handshake failure") ||
		strings.Contains(msg, "remote error") ||
		strings.Contains(msg, "tls: first record") ||
		strings.Contains(msg, "unexpected message")
}

func (s *Sender) sendSTARTTLS(addr, from, to string, msg []byte) error {
	auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	return smtp.SendMail(addr, auth, from, []string{to}, msg)
}

// PoolInfo carries pool quota status for inclusion in alert emails.
type PoolInfo struct {
	TotalAccounts   int
	Exhausted5h     int
	ExhaustedWeekly int
	AvgUsed5h       float64
	AvgUsedWeekly   float64
	EarliestReset   string
	Accounts        []QuotaInfo
}

// QuotaInfo holds a single account's quota state for email rendering.
type QuotaInfo struct {
	Account       string
	FiveHourUsed  float64
	FiveHourReset string
	WeeklyUsed    float64
	WeeklyReset   string
	Error         string // empty if successful, e.g. "upstream status 401"
}

// BuildAlertBody builds the HTML email body for a spend threshold alert.
func BuildAlertBody(userName string, todayCents, thresholdDollars, thresholdCents int64, pool *PoolInfo) string {
	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html><html><head><meta charset="UTF-8"><style>
body{font-family:Arial,sans-serif;padding:20px;color:#333;max-width:700px}
h2{color:#d9534f}
h3{color:#d9534f;margin-top:24px}
h4{margin:16px 0 4px;color:#555}
table{border-collapse:collapse;width:100%;margin:12px 0}
th{background:#f5f5f5;border:1px solid #ddd;padding:8px;text-align:left}
td{border:1px solid #ddd;padding:8px}
.bar-bg{background:#eee;border-radius:8px;height:20px;overflow:hidden}
.bar-fill{height:20px;border-radius:8px;min-width:4px}
.bar-sm-bg{background:#eee;border-radius:4px;height:10px;overflow:hidden}
.bar-sm-fill{height:10px;border-radius:4px;min-width:2px}
.tag{display:inline-block;padding:2px 8px;border-radius:4px;font-size:12px;font-weight:bold}
.tag-ok{background:#e8f5e9;color:#2e7d32}
.tag-exhausted{background:#ffebee;color:#c62828}
.tag-warn{background:#fff3e0;color:#e65100}
.tag-error{background:#f5f5f5;color:#888}
.hint{color:#888;font-size:13px;margin-top:16px}
.footer{color:#999;font-size:12px}
</style></head><body>`)
	sb.WriteString(fmt.Sprintf(`<h2>API 额度使用告警</h2>
<p>用户 <strong>%s</strong>，您好：</p>
<p>您今日的 API 调用消费已触发 <strong>$%d</strong> 告警阈值，请合理分配 Token 使用。</p>
<table>
<tr><th>项目</th><th>数值</th></tr>
<tr><td>今日消费</td><td><strong>$%.2f</strong></td></tr>
<tr><td>触发阈值</td><td><strong>$%d</strong></td></tr>
</table>`, escapeHTML(userName), thresholdDollars, float64(todayCents)/100, thresholdDollars))

	sb.WriteString(`<div style="margin:16px 0;padding:12px 16px;background:#fff3e0;border-left:4px solid #ff9800;color:#663c00">
<p style="margin:0 0 6px"><strong>公共资源使用提醒</strong></p>
<p style="margin:0">当前账号中转池属于公共资源，请合理分配 Token 使用，避免个人消耗过多影响其他用户正常使用。</p>
</div>
<div class="hint">
<p>建议事项：</p>
<ul>
<li>日常任务建议使用 gpt-5.6-luna 或 gpt-5.6-terra；架构设计及需要深度推理的任务请使用 gpt-5.6-sol</li>
<li>检查是否有异常调用或重复请求</li>
<li>可在管理面板查看详细的用量明细和配额设置</li>
</ul>
</div>`)

	// Pool quota visualization
	if pool != nil && pool.TotalAccounts > 0 {
		remaining5h := pool.TotalAccounts - pool.Exhausted5h
		remainingWeekly := pool.TotalAccounts - pool.ExhaustedWeekly

		sb.WriteString(`<hr>`)
		sb.WriteString(fmt.Sprintf(`<h3>当前账号池配额全景</h3>
<p style="color:#888;font-size:13px">池内共 <strong>%d</strong> 个启用账号</p>`, pool.TotalAccounts))

		// 5-hour progress bar
		sb.WriteString(fmt.Sprintf(`<h4>5小时额度</h4>
<p style="margin:2px 0 4px;font-size:13px;color:#555">
  平均使用率 <strong>%.1f%%</strong>
  <span style="float:right">%d 个已耗尽，剩余 %d 个可用</span>
</p>
<div class="bar-bg">
  <div class="bar-fill" style="background:linear-gradient(90deg,%s);width:%.0f%%"></div>
</div>`,
			pool.AvgUsed5h, pool.Exhausted5h, remaining5h,
			progressGradient(pool.AvgUsed5h), clampPercent(pool.AvgUsed5h)))

		// Weekly progress bar
		sb.WriteString(fmt.Sprintf(`<h4>周额度</h4>
<p style="margin:2px 0 4px;font-size:13px;color:#555">
  平均使用率 <strong>%.1f%%</strong>
  <span style="float:right">%d 个已耗尽，剩余 %d 个可用</span>
</p>
<div class="bar-bg">
  <div class="bar-fill" style="background:linear-gradient(90deg,%s);width:%.0f%%"></div>
</div>`,
			pool.AvgUsedWeekly, pool.ExhaustedWeekly, remainingWeekly,
			progressGradient(pool.AvgUsedWeekly), clampPercent(pool.AvgUsedWeekly)))

		if pool.EarliestReset != "" {
			sb.WriteString(fmt.Sprintf(`<p style="color:#888;font-size:13px">最早额度刷新时间：<strong>%s</strong></p>`, escapeHTML(pool.EarliestReset)))
		}

		// Per-account table
		if len(pool.Accounts) > 0 {
			sb.WriteString(`<h4>各账号配额详情</h4>
<table style="font-size:13px">
<tr>
  <th style="text-align:left">账号</th>
  <th style="text-align:center" colspan="2">5h 使用率</th>
  <th style="text-align:center">5h 刷新</th>
  <th style="text-align:center" colspan="2">周使用率</th>
  <th style="text-align:center">周刷新</th>
  <th style="text-align:center">状态</th>
</tr>`)
			for _, a := range pool.Accounts {
				_, statusTag := accountStatus(a)
				fivePct := clampPercent(a.FiveHourUsed)
				fiveLabel := "-"
				if a.FiveHourUsed >= 0 {
					fiveLabel = fmt.Sprintf("%.0f%%", a.FiveHourUsed)
				}
				fiveReset := "-"
				if a.FiveHourReset != "" {
					fiveReset = escapeHTML(a.FiveHourReset)
				}
				weekPct := clampPercent(a.WeeklyUsed)
				weekLabel := "-"
				if a.WeeklyUsed >= 0 {
					weekLabel = fmt.Sprintf("%.0f%%", a.WeeklyUsed)
				}
				weekReset := "-"
				if a.WeeklyReset != "" {
					weekReset = escapeHTML(a.WeeklyReset)
				}

				sb.WriteString(fmt.Sprintf(`<tr>
  <td style="word-break:break-all;max-width:130px">%s</td>
  <td style="text-align:center;white-space:nowrap">%s</td>
  <td style="width:80px">
    <div class="bar-sm-bg">
      <div class="bar-sm-fill" style="background:%s;width:%.0f%%"></div>
    </div>
  </td>
  <td style="text-align:center;white-space:nowrap;font-size:12px">%s</td>
  <td style="text-align:center;white-space:nowrap">%s</td>
  <td style="width:80px">
    <div class="bar-sm-bg">
      <div class="bar-sm-fill" style="background:%s;width:%.0f%%"></div>
    </div>
  </td>
  <td style="text-align:center;white-space:nowrap;font-size:12px">%s</td>
  <td style="text-align:center">%s</td>
</tr>`,
					escapeHTML(a.Account),
					fiveLabel, barColor(a.FiveHourUsed), fivePct, fiveReset,
					weekLabel, barColor(a.WeeklyUsed), weekPct, weekReset,
					statusTag))
			}
			sb.WriteString(`</table>`)
		}
	}

	nextThreshold := ((todayCents / thresholdCents) + 1) * thresholdCents
	sb.WriteString(fmt.Sprintf(`<p class="footer">本邮件由系统自动发送。
今日下一个告警阈值：$%d（消费达 $%d 时触发）</p>`, nextThreshold/100, nextThreshold/100))
	sb.WriteString(`</body></html>`)
	return sb.String()
}

// accountStatus returns display text and HTML tag for an account's quota state.
func accountStatus(a QuotaInfo) (string, string) {
	if a.Error != "" {
		msg := a.Error
		if strings.Contains(msg, "401") {
			return "API Key 失效", `<span class="tag tag-error">Key 失效</span>`
		}
		if strings.Contains(msg, "403") {
			return "无权限", `<span class="tag tag-error">无权限</span>`
		}
		return "不可用", `<span class="tag tag-error">不可用</span>`
	}
	if a.FiveHourUsed >= 100 && a.WeeklyUsed >= 100 {
		return "全部耗尽", `<span class="tag tag-exhausted">全部耗尽</span>`
	}
	if a.FiveHourUsed >= 100 {
		return "5h 耗尽", `<span class="tag tag-exhausted">5h耗尽</span>`
	}
	if a.WeeklyUsed >= 100 {
		return "周耗尽", `<span class="tag tag-exhausted">周耗尽</span>`
	}
	if a.FiveHourUsed >= 80 || a.WeeklyUsed >= 80 {
		return "即将耗尽", `<span class="tag tag-warn">即将耗尽</span>`
	}
	return "正常", `<span class="tag tag-ok">正常</span>`
}

// progressGradient returns a CSS gradient string for a progress bar.
func progressGradient(pct float64) string {
	switch {
	case pct >= 90:
		return "#ff5722,#d32f2f"
	case pct >= 60:
		return "#ff9800,#ff5722"
	case pct >= 30:
		return "#ffc107,#ff9800"
	default:
		return "#4caf50,#8bc34a"
	}
}

// barColor returns a solid color for a small progress bar.
func barColor(pct float64) string {
	if pct < 0 {
		return "#ccc"
	}
	switch {
	case pct >= 90:
		return "#d32f2f"
	case pct >= 60:
		return "#ff5722"
	case pct >= 30:
		return "#ff9800"
	default:
		return "#4caf50"
	}
}

// clampPercent returns the percentage clamped to [1, 100] for bar width display.
// Returns 0 if pct < 0 (unknown).
func clampPercent(pct float64) float64 {
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	if pct < 1 {
		return 1 // show a tiny sliver instead of 0
	}
	return pct
}

// AccountQuotaInfo holds usage data for a single account, used in email rendering.
type AccountQuotaInfo struct {
	Account      string
	FiveHourUsed float64
	FiveHourReset string
	WeeklyUsed   float64
	WeeklyReset  string
	Error        string // empty if successful
}

// BuildPoolExhaustedBody builds the HTML email body for pool quota exhaustion notification.
func BuildPoolExhaustedBody(windowType string, earliestReset string, results []AccountQuotaInfo) string {
	var sb strings.Builder
	windowLabel := "5 小时额度"
	if windowType == "weekly" {
		windowLabel = "周额度"
	}
	sb.WriteString(`<!DOCTYPE html><html><head><meta charset="UTF-8"><style>
body{font-family:Arial,sans-serif;padding:20px;color:#333;max-width:700px}
h2{color:#d9534f}
h3{color:#d9534f;margin-top:24px}
h4{margin:16px 0 4px;color:#555}
table{border-collapse:collapse;width:100%;margin:12px 0}
th{background:#f5f5f5;border:1px solid #ddd;padding:8px;text-align:left}
td{border:1px solid #ddd;padding:8px}
.bar-sm-bg{background:#eee;border-radius:4px;height:10px;overflow:hidden}
.bar-sm-fill{height:10px;border-radius:4px;min-width:2px}
.tag{display:inline-block;padding:2px 8px;border-radius:4px;font-size:12px;font-weight:bold}
.tag-ok{background:#e8f5e9;color:#2e7d32}
.tag-exhausted{background:#ffebee;color:#c62828}
.tag-error{background:#f5f5f5;color:#888}
.hint{color:#888;font-size:13px;margin-top:16px}
.footer{color:#999;font-size:12px}
</style></head><body>`)
	sb.WriteString(fmt.Sprintf(`<h2>API 服务池额度耗尽通知</h2>
<p>各位用户，您好：</p>
<p>当前 API 服务池中所有账号的 <strong>%s</strong> 已全部耗尽。</p>
<p>在额度刷新之前，部分请求可能无法正常处理。</p>
<table>
<tr><td>耗尽项</td><td><strong>%s</strong></td></tr>
<tr><td>预计重置时间</td><td><strong>%s</strong></td></tr>
</table>`, windowLabel, windowLabel, escapeHTML(earliestReset)))

	if len(results) > 0 {
		sb.WriteString(`<h4>各账号额度详情</h4>
<table style="font-size:13px">
<tr>
  <th style="text-align:left">账号</th>
  <th style="text-align:center" colspan="2">5h 使用率</th>
  <th style="text-align:center">5h 刷新</th>
  <th style="text-align:center" colspan="2">周使用率</th>
  <th style="text-align:center">周刷新</th>
  <th style="text-align:center">状态</th>
</tr>`)
		for _, r := range results {
			status := `<span class="tag tag-ok">正常</span>`
			if r.Error != "" {
				if strings.Contains(r.Error, "401") {
					status = `<span class="tag tag-error">Key 失效</span>`
				} else {
					status = `<span class="tag tag-error">不可用</span>`
				}
			} else if r.FiveHourUsed >= 100 && r.WeeklyUsed >= 100 {
				status = `<span class="tag tag-exhausted">全部耗尽</span>`
			} else if r.FiveHourUsed >= 100 {
				status = `<span class="tag tag-exhausted">5h耗尽</span>`
			} else if r.WeeklyUsed >= 100 {
				status = `<span class="tag tag-exhausted">周耗尽</span>`
			} else if r.FiveHourUsed >= 80 || r.WeeklyUsed >= 80 {
				status = `<span class="tag" style="background:#fff3e0;color:#e65100">即将耗尽</span>`
			}

			fivePct := clampPercent(r.FiveHourUsed)
			fiveLabel := "-"
			if r.FiveHourUsed >= 0 {
				fiveLabel = fmt.Sprintf("%.0f%%", r.FiveHourUsed)
			}
			fiveReset := "-"
			if r.FiveHourReset != "" {
				fiveReset = escapeHTML(r.FiveHourReset)
			}
			weekPct := clampPercent(r.WeeklyUsed)
			weekLabel := "-"
			if r.WeeklyUsed >= 0 {
				weekLabel = fmt.Sprintf("%.0f%%", r.WeeklyUsed)
			}
			weekReset := "-"
			if r.WeeklyReset != "" {
				weekReset = escapeHTML(r.WeeklyReset)
			}

			sb.WriteString(fmt.Sprintf(`<tr>
  <td style="word-break:break-all;max-width:130px">%s</td>
  <td style="text-align:center;white-space:nowrap">%s</td>
  <td style="width:80px">
    <div class="bar-sm-bg"><div class="bar-sm-fill" style="background:%s;width:%.0f%%"></div></div>
  </td>
  <td style="text-align:center;white-space:nowrap;font-size:12px">%s</td>
  <td style="text-align:center;white-space:nowrap">%s</td>
  <td style="width:80px">
    <div class="bar-sm-bg"><div class="bar-sm-fill" style="background:%s;width:%.0f%%"></div></div>
  </td>
  <td style="text-align:center;white-space:nowrap;font-size:12px">%s</td>
  <td style="text-align:center">%s</td>
</tr>`,
				escapeHTML(r.Account),
				fiveLabel, barColor(r.FiveHourUsed), fivePct, fiveReset,
				weekLabel, barColor(r.WeeklyUsed), weekPct, weekReset,
				status))
		}
		sb.WriteString(`</table>`)
	}

	sb.WriteString(`<div class="hint">
<p>建议事项：</p>
<ul>
<li>请耐心等待额度自动刷新</li>
<li>简单任务请优先选用轻量模型，降低 Token 消耗</li>
</ul>
</div>`)
	sb.WriteString(`<p class="footer">本邮件由系统自动发送，请勿回复。</p>`)
	sb.WriteString(`</body></html>`)
	return sb.String()
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}
