// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultsWhenNoSources(t *testing.T) {
	// Clear any relevant env vars first.
	for _, k := range []string{
		"SERVER_PORT", "DB_HOST", "DB_NAME", "REDIS_HOST", "APP_ENV",
		"LOG_LEVEL", "DATASOURCE_PROVIDER",
	} {
		os.Unsetenv(k)
	}

	l, err := NewLoader("", "")
	require.NoError(t, err)
	c, err := l.Load()
	require.NoError(t, err)

	assert.Equal(t, 8080, c.Server.Port)
	assert.Equal(t, "0.0.0.0", c.Server.Host)
	assert.Equal(t, "/api/v1", c.Server.APIBasePath)
	assert.Equal(t, "127.0.0.1", c.DB.Host)
	assert.Equal(t, 3306, c.DB.Port)
	assert.Equal(t, "astock_selector", c.DB.Name)
	assert.Equal(t, "mysql", c.DB.Driver)
	assert.Equal(t, "127.0.0.1", c.Redis.Host)
	assert.Equal(t, 6379, c.Redis.Port)
	assert.Equal(t, "akshare", c.Datasource.Provider)
	assert.Equal(t, 30*time.Second, c.Server.ReadTimeout)
	assert.True(t, c.IsDev())
}

func TestEnvOverridesDefaults(t *testing.T) {
	t.Setenv("SERVER_PORT", "9090")
	t.Setenv("DB_HOST", "10.20.30.40")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATASOURCE_PROVIDER", "tushare")
	t.Setenv("DATASOURCE_TOKEN", "abc-123")
	t.Setenv("REDIS_PASSWORD", "redis-pw")

	l, err := NewLoader("", "")
	require.NoError(t, err)
	c, err := l.Load()
	require.NoError(t, err)

	assert.Equal(t, 9090, c.Server.Port)
	assert.Equal(t, "10.20.30.40", c.DB.Host)
	assert.Equal(t, "secret", c.DB.Password)
	assert.Equal(t, "tushare", c.Datasource.Provider)
	assert.Equal(t, "abc-123", c.Datasource.Token)
	assert.Equal(t, "redis-pw", c.Redis.Password)
	assert.False(t, c.IsDev())
}

func TestInvalidServerPortFails(t *testing.T) {
	t.Setenv("SERVER_PORT", "70000")
	l, err := NewLoader("", "")
	require.NoError(t, err)
	_, err = l.Load()
	assert.Error(t, err, "expected validation failure for out-of-range port")
}

func TestDSN(t *testing.T) {
	d := DBConfig{
		Host:      "db.example.com",
		Port:      3306,
		User:      "u",
		Password:  "p",
		Name:      "n",
		Charset:   "utf8mb4",
		ParseTime: true,
		Loc:       "Local",
	}
	assert.Contains(t, d.DSN(), "u:p@tcp(db.example.com:3306)/n")
	assert.Contains(t, d.DSN(), "parseTime=True")
}

func TestPostgresDSN(t *testing.T) {
	d := DBConfig{
		Host:     "pg.example.com",
		Port:     5432,
		User:     "u",
		Password: "p",
		Name:     "n",
		SSLMode:  "require",
		Loc:      "Asia/Shanghai",
	}
	dsn := d.PostgresDSN()
	assert.Contains(t, dsn, "host=pg.example.com")
	assert.Contains(t, dsn, "port=5432")
	assert.Contains(t, dsn, "user=u")
	assert.Contains(t, dsn, "dbname=n")
	assert.Contains(t, dsn, "sslmode=require")
	assert.Contains(t, dsn, "TimeZone=Asia/Shanghai")
}

func TestPostgresDSN_Defaults(t *testing.T) {
	// Empty SSLMode → "disable"; empty Loc → "UTC".
	d := DBConfig{Host: "h", Port: 5432, User: "u", Password: "p", Name: "n"}
	dsn := d.PostgresDSN()
	assert.Contains(t, dsn, "sslmode=disable")
	assert.Contains(t, dsn, "TimeZone=UTC")
}

func TestYAMLConfigFile(t *testing.T) {
	yaml := []byte(`
app:
  env: production
server:
  port: 7777
db:
  host: yaml-host
  port: 33306
`)
	path := writeTempYAML(t, yaml)

	l, err := NewLoader(path, "")
	require.NoError(t, err)
	c, err := l.Load()
	require.NoError(t, err)

	assert.Equal(t, "production", c.App.Env)
	assert.Equal(t, 7777, c.Server.Port)
	assert.Equal(t, "yaml-host", c.DB.Host)
	assert.Equal(t, 33306, c.DB.Port)
}

func writeTempYAML(t *testing.T, b []byte) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	require.NoError(t, err)
	_, err = f.Write(b)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

func TestNotifyChannelDefaults(t *testing.T) {
	// Clear any env that could override our defaults.
	for _, k := range []string{
		"NOTIFY_CHANNEL_PRIMARY", "NOTIFY_CHANNEL_RETRY_ATTEMPTS",
		"NOTIFY_CHANNEL_FEISHU_ENABLED", "NOTIFY_CHANNEL_DINGTALK_ENABLED",
		"NOTIFY_CHANNEL_WECOM_ENABLED",
		"NOTIFY_CHANNEL_CB_FAILURE_THRESHOLD", "NOTIFY_CHANNEL_CB_WINDOW",
		"NOTIFY_CHANNEL_CB_COOLDOWN",
	} {
		os.Unsetenv(k)
	}

	l, err := NewLoader("", "")
	require.NoError(t, err)
	c, err := l.Load()
	require.NoError(t, err)

	assert.Equal(t, "feishu", c.Notify.Channel.Primary)
	assert.Equal(t, 3, c.Notify.Channel.RetryAttempts)
	assert.Equal(t, 5*time.Second, c.Notify.Channel.Feishu.Timeout)
	assert.Equal(t, 5*time.Second, c.Notify.Channel.DingTalk.Timeout)
	assert.Equal(t, 5*time.Second, c.Notify.Channel.WeCom.Timeout)
	assert.False(t, c.Notify.Channel.Feishu.Enabled)
	assert.False(t, c.Notify.Channel.DingTalk.Enabled)
	assert.False(t, c.Notify.Channel.WeCom.Enabled)
	assert.Equal(t, 5, c.Notify.Channel.CircuitBreaker.FailureThreshold)
	assert.Equal(t, 5*time.Minute, c.Notify.Channel.CircuitBreaker.Window)
	assert.Equal(t, 10*time.Minute, c.Notify.Channel.CircuitBreaker.Cooldown)
}

func TestNotifyChannelEnvOverrides(t *testing.T) {
	t.Setenv("NOTIFY_CHANNEL_PRIMARY", "dingtalk")
	t.Setenv("NOTIFY_CHANNEL_RETRY_ATTEMPTS", "5")
	t.Setenv("NOTIFY_CHANNEL_FEISHU_ENABLED", "true")
	t.Setenv("NOTIFY_CHANNEL_FEISHU_WEBHOOK_URL", "https://open.feishu.cn/hook/abc")
	t.Setenv("NOTIFY_CHANNEL_FEISHU_SECRET", "topsecret")
	t.Setenv("NOTIFY_CHANNEL_DINGTALK_WEBHOOK_URL", "https://oapi.dingtalk.com/robot/send?access_token=x")
	t.Setenv("NOTIFY_CHANNEL_DINGTALK_SECRET", "ding-secret")
	t.Setenv("NOTIFY_CHANNEL_WECOM_ENABLED", "true")
	t.Setenv("NOTIFY_CHANNEL_WECOM_WEBHOOK_URL", "https://qyapi.weixin.qq.com/hook/y")
	t.Setenv("NOTIFY_CHANNEL_CB_FAILURE_THRESHOLD", "7")
	t.Setenv("NOTIFY_CHANNEL_CB_WINDOW", "3m")
	t.Setenv("NOTIFY_CHANNEL_CB_COOLDOWN", "30m")

	l, err := NewLoader("", "")
	require.NoError(t, err)
	c, err := l.Load()
	require.NoError(t, err)

	assert.Equal(t, "dingtalk", c.Notify.Channel.Primary)
	assert.Equal(t, 5, c.Notify.Channel.RetryAttempts)
	assert.True(t, c.Notify.Channel.Feishu.Enabled)
	assert.Equal(t, "https://open.feishu.cn/hook/abc", c.Notify.Channel.Feishu.WebhookURL)
	assert.Equal(t, "topsecret", c.Notify.Channel.Feishu.Secret)
	assert.Equal(t, "https://oapi.dingtalk.com/robot/send?access_token=x", c.Notify.Channel.DingTalk.WebhookURL)
	assert.Equal(t, "ding-secret", c.Notify.Channel.DingTalk.Secret)
	assert.True(t, c.Notify.Channel.WeCom.Enabled)
	assert.Equal(t, "https://qyapi.weixin.qq.com/hook/y", c.Notify.Channel.WeCom.WebhookURL)
	assert.Equal(t, 7, c.Notify.Channel.CircuitBreaker.FailureThreshold)
	assert.Equal(t, 3*time.Minute, c.Notify.Channel.CircuitBreaker.Window)
	assert.Equal(t, 30*time.Minute, c.Notify.Channel.CircuitBreaker.Cooldown)
}

func TestNotifyChannelYAML(t *testing.T) {
	yaml := []byte(`
notify:
  channel:
    primary: wecom
    fallback: [feishu, dingtalk]
    retry_attempts: 7
    feishu:
      enabled: true
      webhook_url: "https://example/feishu"
      secret: "fs"
      timeout: 8s
    dingtalk:
      enabled: true
      webhook_url: "https://example/dingtalk"
    wecom:
      enabled: true
      webhook_url: "https://example/wecom"
    circuit_breaker:
      failure_threshold: 9
      window: 11m
      cooldown: 22m
`)
	path := writeTempYAML(t, yaml)
	l, err := NewLoader(path, "")
	require.NoError(t, err)
	c, err := l.Load()
	require.NoError(t, err)

	assert.Equal(t, "wecom", c.Notify.Channel.Primary)
	assert.Equal(t, []string{"feishu", "dingtalk"}, c.Notify.Channel.Fallback)
	assert.Equal(t, 7, c.Notify.Channel.RetryAttempts)
	assert.True(t, c.Notify.Channel.Feishu.Enabled)
	assert.Equal(t, "https://example/feishu", c.Notify.Channel.Feishu.WebhookURL)
	assert.Equal(t, "fs", c.Notify.Channel.Feishu.Secret)
	assert.Equal(t, 8*time.Second, c.Notify.Channel.Feishu.Timeout)
	assert.Equal(t, 9, c.Notify.Channel.CircuitBreaker.FailureThreshold)
	assert.Equal(t, 11*time.Minute, c.Notify.Channel.CircuitBreaker.Window)
	assert.Equal(t, 22*time.Minute, c.Notify.Channel.CircuitBreaker.Cooldown)
}

func TestPostgresDSN_LocalTimezoneFallsBackToUTC(t *testing.T) {
	// Loc="Local" is the MySQL-driver default; Postgres rejects it.
	// PostgresDSN must coerce it (and "") to UTC.
	d := DBConfig{Host: "h", Port: 5432, User: "u", Password: "p", Name: "n", Loc: "Local"}
	assert.Contains(t, d.PostgresDSN(), "TimeZone=UTC")
	assert.NotContains(t, d.PostgresDSN(), "TimeZone=Local")
}
