// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package notify

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// BuildMorningPick produces a Message for the pre-open / morning
// recommendation card. Title is "早盘推荐" by convention; Content is a
// Markdown table with one row per pick.
//
// stocks may be empty; in that case the message clearly says "no picks
// today" so the recipient's chat client doesn't render an empty card.
func BuildMorningPick(stocks []StockPick) *Message {
	title := "早盘推荐"
	ts := time.Now()
	if len(stocks) == 0 {
		return &Message{
			Title:     title,
			Content:   "今日无符合策略的早盘推荐。",
			Type:      MsgMorning,
			Timestamp: ts,
		}
	}
	return &Message{
		Title:     title,
		Content:   renderPickTable(stocks, fmt.Sprintf("共 %d 只早盘推荐（%s）", len(stocks), ts.Format("2006-01-02"))),
		Type:      MsgMorning,
		Meta:      picksToMeta(stocks),
		Timestamp: ts,
	}
}

// BuildAfternoonPick produces a Message for the closing / afternoon
// recommendation card. Title is "尾盘推荐".
func BuildAfternoonPick(stocks []StockPick) *Message {
	title := "尾盘推荐"
	ts := time.Now()
	if len(stocks) == 0 {
		return &Message{
			Title:     title,
			Content:   "今日无符合策略的尾盘推荐。",
			Type:      MsgAfternoon,
			Timestamp: ts,
		}
	}
	return &Message{
		Title:     title,
		Content:   renderPickTable(stocks, fmt.Sprintf("共 %d 只尾盘推荐（%s）", len(stocks), ts.Format("2006-01-02"))),
		Type:      MsgAfternoon,
		Meta:      picksToMeta(stocks),
		Timestamp: ts,
	}
}

// BuildDailyReview produces the end-of-day review card. The summary line
// in Content mentions hits and the date so the message is self-describing
// when shown in a chat history scroll.
//
// hits is the number of recommendations that hit their T+1 target on
// the given date. topGainers / topLosers are the day's standout picks.
func BuildDailyReview(date string, hits int, topGainers, topLosers []StockPick) *Message {
	title := "每日复盘"
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	ts, _ := time.Parse("2006-01-02", date)
	if ts.IsZero() {
		ts = time.Now()
	}

	var b strings.Builder
	fmt.Fprintf(&b, "### 每日复盘（%s）\n\n", date)
	fmt.Fprintf(&b, "- 命中数：**%d**\n", hits)
	fmt.Fprintf(&b, "\n#### 当日涨幅 TOP %d\n\n", len(topGainers))
	b.WriteString(renderPickTable(topGainers, ""))
	fmt.Fprintf(&b, "\n#### 当日跌幅 TOP %d\n\n", len(topLosers))
	b.WriteString(renderPickTable(topLosers, ""))

	return &Message{
		Title:     title,
		Content:   b.String(),
		Type:      MsgReview,
		Meta: map[string]any{
			"date":         date,
			"hits":         hits,
			"top_gainers":  topGainers,
			"top_losers":   topLosers,
		},
		Timestamp: ts,
	}
}

// BuildAlert produces a system alert card. source is the alert origin
// (e.g. "datasource", "executor"); severity is "info" | "warn" | "error";
// detail is a free-form human-readable description.
func BuildAlert(source, severity, detail string) *Message {
	if source == "" {
		source = "system"
	}
	if severity == "" {
		severity = "error"
	}
	return &Message{
		Title: fmt.Sprintf("[%s] %s", strings.ToUpper(severity), source),
		Content: fmt.Sprintf(
			"> **告警**\n>\n> - 来源：`%s`\n> - 级别：`%s`\n> - 时间：%s\n>\n> %s",
			source, severity, time.Now().Format(time.RFC3339), detail,
		),
		Type:      MsgAlert,
		Meta: map[string]any{
			"source":   source,
			"severity": severity,
		},
		Timestamp: time.Now(),
	}
}

// renderPickTable formats stocks as a Markdown table. summary is an
// optional caption placed above the table; pass "" to omit it.
//
// The function is deterministic: the slice is sorted by code first so
// repeated calls with the same input produce identical output (useful
// for tests and stable message IDs).
func renderPickTable(stocks []StockPick, summary string) string {
	if len(stocks) == 0 {
		return "（无数据）"
	}
	// Sort a copy; do not mutate the caller's slice.
	sorted := make([]StockPick, len(stocks))
	copy(sorted, stocks)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Code < sorted[j].Code })

	var b strings.Builder
	if summary != "" {
		fmt.Fprintf(&b, "**%s**\n\n", summary)
	}
	b.WriteString("| 代码 | 名称 | 策略 | 建议区间 |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, s := range sorted {
		name := s.Name
		if name == "" {
			name = "-"
		}
		strategy := s.Strategy
		if strategy == "" {
			strategy = "-"
		}
		range_ := strings.TrimSpace(s.EntryLow + " - " + s.EntryHigh)
		if range_ == "-" {
			range_ = "-"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", s.Code, name, strategy, range_)
	}
	return b.String()
}

func picksToMeta(stocks []StockPick) map[string]any {
	codes := make([]string, 0, len(stocks))
	for _, s := range stocks {
		codes = append(codes, s.Code)
	}
	return map[string]any{"codes": codes, "count": len(stocks)}
}
