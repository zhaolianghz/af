// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package notify

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func samplePicks() []StockPick {
	return []StockPick{
		{Code: "600519", Name: "Kweichow Moutai", Strategy: "morning_breakout", EntryLow: "1700.00", EntryHigh: "1750.00"},
		{Code: "000858", Name: "Wuliangye", Strategy: "afternoon_main_inflow", EntryLow: "160.00", EntryHigh: "165.00"},
		{Code: "300750", Name: "CATL", Strategy: "macd_golden_cross", EntryLow: "200.00", EntryHigh: "210.00"},
	}
}

func TestBuildMorningPick_NonEmptyStocks(t *testing.T) {
	msg := BuildMorningPick(samplePicks())
	require.NotNil(t, msg)
	assert.Equal(t, MsgMorning, msg.Type)
	assert.Equal(t, "早盘推荐", msg.Title)
	assert.NotEmpty(t, msg.Content)
	assert.Contains(t, msg.Content, "600519")
	assert.Contains(t, msg.Content, "000858")
	assert.Contains(t, msg.Content, "300750")
	// Content is a Markdown table.
	assert.Contains(t, msg.Content, "|")
	// Sorted by code ascending.
	assert.Less(t, strings.Index(msg.Content, "000858"), strings.Index(msg.Content, "300750"))
	assert.Less(t, strings.Index(msg.Content, "000858"), strings.Index(msg.Content, "600519"))
	// Timestamp must be set.
	assert.WithinDuration(t, time.Now(), msg.Timestamp, 5*time.Second)
	// Meta must carry the codes.
	codes, ok := msg.Meta["codes"].([]string)
	require.True(t, ok, "Meta should contain codes slice")
	assert.Equal(t, []string{"600519", "000858", "300750"}, codes)
}

func TestBuildMorningPick_EmptyStocks(t *testing.T) {
	msg := BuildMorningPick(nil)
	require.NotNil(t, msg)
	assert.Equal(t, MsgMorning, msg.Type)
	assert.Equal(t, "早盘推荐", msg.Title)
	assert.Contains(t, msg.Content, "无")
}

func TestBuildAfternoonPick_NonEmptyStocks(t *testing.T) {
	msg := BuildBuildAfternoonLikeMsg(samplePicks())
	require.NotNil(t, msg)
	assert.Equal(t, MsgAfternoon, msg.Type)
	assert.Equal(t, "尾盘推荐", msg.Title)
	assert.NotEmpty(t, msg.Content)
	assert.Contains(t, msg.Content, "600519")
}

func TestBuildAfternoonPick_EmptyStocks(t *testing.T) {
	msg := BuildAfternoonPick(nil)
	require.NotNil(t, msg)
	assert.Equal(t, MsgAfternoon, msg.Type)
	assert.Contains(t, msg.Content, "无")
}

func TestBuildDailyReview(t *testing.T) {
	gainers := []StockPick{
		{Code: "601318", Name: "Ping An", Strategy: "morning_breakout", EntryLow: "50", EntryHigh: "52"},
	}
	losers := []StockPick{
		{Code: "601398", Name: "ICBC", Strategy: "low_pe", EntryLow: "5", EntryHigh: "5.5"},
	}
	msg := BuildDailyReview("2026-06-10", 7, gainers, losers)
	require.NotNil(t, msg)
	assert.Equal(t, MsgReview, msg.Type)
	assert.Equal(t, "每日复盘", msg.Title)
	assert.Contains(t, msg.Content, "2026-06-10")
	assert.Contains(t, msg.Content, "命中数")
	assert.Contains(t, msg.Content, "**7**")
	assert.Contains(t, msg.Content, "601318")
	assert.Contains(t, msg.Content, "601398")
	assert.Contains(t, msg.Content, "TOP 1")
	assert.Contains(t, msg.Content, "TOP 1")
}

func TestBuildDailyReview_DefaultsDateToToday(t *testing.T) {
	msg := BuildDailyReview("", 0, nil, nil)
	require.NotNil(t, msg)
	assert.Equal(t, MsgReview, msg.Type)
	// Today's date should appear in the content.
	now := time.Now().Format("2006-01-02")
	assert.Contains(t, msg.Content, now)
}

func TestBuildAlert(t *testing.T) {
	msg := BuildAlert("datasource", "error", "eastmoney and sina both returned 5xx")
	require.NotNil(t, msg)
	assert.Equal(t, MsgAlert, msg.Type)
	assert.Contains(t, msg.Title, "datasource")
	assert.Contains(t, msg.Content, "datasource")
	assert.Contains(t, msg.Content, "error")
	assert.Contains(t, msg.Content, "eastmoney and sina both returned 5xx")
	assert.Equal(t, "datasource", msg.Meta["source"])
	assert.Equal(t, "error", msg.Meta["severity"])
}

func TestBuildAlert_EmptyFieldsGetDefaults(t *testing.T) {
	msg := BuildAlert("", "", "something happened")
	require.NotNil(t, msg)
	assert.Equal(t, "system", msg.Meta["source"])
	assert.Equal(t, "error", msg.Meta["severity"])
	assert.Contains(t, msg.Title, "ERROR")
}

// BuildBuildAfternoonLikeMsg is just a thin test wrapper around
// BuildAfternoonPick used by the test above.
func BuildBuildAfternoonLikeMsg(stocks []StockPick) *Message {
	return BuildAfternoonPick(stocks)
}
