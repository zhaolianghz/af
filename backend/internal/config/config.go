// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package config loads application configuration from YAML + .env + real env vars.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// Config is the root configuration object.
type Config struct {
	App        AppConfig        `mapstructure:"app"`
	Server     ServerConfig     `mapstructure:"server"`
	DB         DBConfig         `mapstructure:"db"`
	Redis      RedisConfig      `mapstructure:"redis"`
	Datasource DatasourceConfig `mapstructure:"datasource"`
	Notify     NotifyConfig     `mapstructure:"notify"`
	Cron       CronConfig       `mapstructure:"cron"`
	Executor   ExecutorConfig   `mapstructure:"executor"`
	Calendar   CalendarConfig   `mapstructure:"calendar"`
	Perf       PerfConfig       `mapstructure:"perf"`
	Log        LogConfig        `mapstructure:"log"`
	AI         AIConfig         `mapstructure:"ai"`
	Review     ReviewConfig     `mapstructure:"review"`
	Auth       AuthConfig       `mapstructure:"auth"`
}

type AppConfig struct {
	Name    string `mapstructure:"name"`
	Env     string `mapstructure:"env"`
	Version string `mapstructure:"version"`
}

type ServerConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	APIBasePath     string        `mapstructure:"api_base_path"`
}

type DBConfig struct {
	Driver          string        `mapstructure:"driver"`
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	User            string        `mapstructure:"user"`
	Password        string        `mapstructure:"password"`
	Name            string        `mapstructure:"name"`
	Charset         string        `mapstructure:"charset"`
	SSLMode         string        `mapstructure:"sslmode"` // postgres only: disable|require|verify-ca|verify-full (default "disable")
	ParseTime       bool          `mapstructure:"parse_time"`
	Loc             string        `mapstructure:"loc"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	LogLevel        string        `mapstructure:"log_level"`
}

type RedisConfig struct {
	Host         string `mapstructure:"host"`
	Port         int    `mapstructure:"port"`
	Password     string `mapstructure:"password"`
	DB           int    `mapstructure:"db"`
	PoolSize     int    `mapstructure:"pool_size"`
	MinIdleConns int    `mapstructure:"min_idle_conns"`
}

type DatasourceConfig struct {
	// Provider is the legacy single-provider name (kept for
	// backwards compatibility with the A1 skeleton). New code
	// should use Sources.
	Provider  string        `mapstructure:"provider"`
	Token     string        `mapstructure:"token"`
	Timeout   time.Duration `mapstructure:"timeout"`
	Retry     int           `mapstructure:"retry"`
	CacheTTL  time.Duration `mapstructure:"cache_ttl"`
	RateLimit int           `mapstructure:"rate_limit"`

	// Sources is the A3 multi-source chain: primary, then
	// fallbacks in order. A disabled source is skipped at
	// registration time.
	Sources []SourceConfig `mapstructure:"sources"`

	// FailThreshold / FailWindow / Cooldown configure the
	// per-source circuit breaker.
	FailThreshold int           `mapstructure:"fail_threshold"`
	FailWindow    time.Duration `mapstructure:"fail_window"`
	Cooldown      time.Duration `mapstructure:"cooldown"`

	// ConsistencyThreshold is the relative price deviation
	// above which cross-source validation flags a quote pair.
	ConsistencyThreshold float64 `mapstructure:"consistency_threshold"`
}

// SourceConfig is one entry in the A3 multi-source chain. The order
// in config.yaml determines the manager's call order.
type SourceConfig struct {
	// Name is the registered source identifier (e.g. "eastmoney",
	// "sina", "akshare"). It is matched against the registry
	// during wiring.
	Name string `mapstructure:"name"`
	// BaseURL overrides the source's default endpoint. Useful for
	// staging mirrors and for tests.
	BaseURL string `mapstructure:"base_url"`
	// Enabled lets operators turn a configured source off
	// without removing it from YAML.
	Enabled bool `mapstructure:"enabled"`
	// Timeout overrides the per-call HTTP timeout for this
	// source.
	Timeout time.Duration `mapstructure:"timeout"`
}

// NotifyConfig groups the notification subsystem settings. The legacy
// Email/WeChat/Feishu flat fields are preserved for backwards
// compatibility; the A4 channel-based config lives under Channel.
type NotifyConfig struct {
	Channels       []string `mapstructure:"channels"`
	DefaultChannel string   `mapstructure:"default_channel"`
	// Email is the legacy SMTP-based notifier, kept for backwards
	// compatibility with the A1 skeleton. A4 only consumes
	// ChannelConfigs keyed by feishu / dingtalk / wecom.
	Email  EmailNotify  `mapstructure:"email"`
	WeChat WeChatNotify `mapstructure:"wechat"`
	Feishu FeishuNotify `mapstructure:"feishu"`
	// Channel is the A4 multi-channel config (webhook URLs, secrets,
	// timeouts, retry, circuit breaker).
	Channel NotifyChannelConfig `mapstructure:"channel"`
}

// NotifyChannelConfig holds the A4 channel + reliability config.
type NotifyChannelConfig struct {
	// Feishu / DingTalk / WeCom are individually keyed so operators
	// can wire one, two, or all three channels in any order.
	Feishu   ChannelConfig `mapstructure:"feishu"`
	DingTalk ChannelConfig `mapstructure:"dingtalk"`
	WeCom    ChannelConfig `mapstructure:"wecom"`
	// Primary is the channel name to try first. Fallback is walked in
	// order when the primary fails. Both default to "feishu" / [].
	Primary  string   `mapstructure:"primary"`
	Fallback []string `mapstructure:"fallback"`
	// RetryAttempts is the total number of tries per channel
	// (including the first). Default 3.
	RetryAttempts int `mapstructure:"retry_attempts"`
	// CircuitBreaker controls per-channel open/close behavior.
	CircuitBreaker CircuitBreakerConfig `mapstructure:"circuit_breaker"`
}

// ChannelConfig configures a single delivery channel. WebhookURL is
// required; Secret is only used by Feishu/DingTalk; Timeout is the
// per-attempt HTTP timeout (the manager also wraps each call in a
// context deadline).
type ChannelConfig struct {
	WebhookURL string        `mapstructure:"webhook_url"`
	Secret     string        `mapstructure:"secret"`
	Timeout    time.Duration `mapstructure:"timeout"`
	// Enabled lets operators turn a configured channel off without
	// removing it from the YAML. Disabled channels are skipped during
	// registration.
	Enabled bool `mapstructure:"enabled"`
}

// CircuitBreakerConfig is exposed to YAML so operators can tune the
// breaker per environment.
type CircuitBreakerConfig struct {
	FailureThreshold int           `mapstructure:"failure_threshold"`
	Window           time.Duration `mapstructure:"window"`
	Cooldown         time.Duration `mapstructure:"cooldown"`
}

type EmailNotify struct {
	SMTPHost string `mapstructure:"smtp_host"`
	SMTPPort int    `mapstructure:"smtp_port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	From     string `mapstructure:"from"`
}

type WeChatNotify struct {
	CorpID     string `mapstructure:"corpid"`
	AgentID    string `mapstructure:"agentid"`
	CorpSecret string `mapstructure:"corpsecret"`
}

type FeishuNotify struct {
	Webhook string `mapstructure:"webhook"`
}

type CronConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	Timezone       string `mapstructure:"timezone"`
	DailySelection string `mapstructure:"daily_selection"`
}

// ExecutorConfig groups the A7 run-level executor settings.
//
// MaxConcurrency caps the per-run concurrent node goroutines
// (passed through to orchestrator.Executor). DefaultRunTimeout is
// the per-node ceiling; runs that exceed it are cancelled. SSE
// keeps each stream open until the run reaches a terminal status
// (success / failed / skipped) or the client disconnects.
type ExecutorConfig struct {
	MaxConcurrency    int           `mapstructure:"max_concurrency"`
	DefaultRunTimeout time.Duration `mapstructure:"default_run_timeout"`
	SchedulerEnabled  bool          `mapstructure:"scheduler_enabled"`
	// SSEHeartbeat is the interval between keep-alive comments
	// pushed while a client is connected but no new event has
	// arrived. Browsers and proxies tend to time out idle
	// connections around 30-60s.
	SSEHeartbeat time.Duration `mapstructure:"sse_heartbeat"`
	// SSEMaxBuffer is the per-run event channel buffer. When
	// full, additional events are dropped (and counted).
	SSEMaxBuffer int `mapstructure:"sse_max_buffer"`
}

// CalendarConfig holds trading-day + trading-session rules. v1
// uses a static model: weekends are non-trading, the morning
// session runs 09:30-11:30 and the afternoon session 13:00-15:00
// (Asia/Shanghai). Holiday overrides are loaded from the
// trading_calendar table seeded at boot.
type CalendarConfig struct {
	Timezone       string `mapstructure:"timezone"`
	MorningStart   string `mapstructure:"morning_start"`
	MorningEnd     string `mapstructure:"morning_end"`
	AfternoonStart string `mapstructure:"afternoon_start"`
	AfternoonEnd   string `mapstructure:"afternoon_end"`
	ReviewStart    string `mapstructure:"review_start"`
	ReviewEnd      string `mapstructure:"review_end"`
}

// PerfConfig groups the §9 post-hoc performance engine settings.
//
//   - Enabled gates the HTTP routes (/api/v1/perf/*). When false the
//     package still loads but no route is mounted.
//   - BackfillCron is the end-of-day schedule that walks every
//     recommendation and (re-)computes its snapshot. Defaults to
//     "30 15 * * 1-5" (15:30 Mon-Fri, after the A-share close at 15:00).
//     Set to "" to disable the cron.
//   - KlineLookback / KlineTimeout / KlineFuturePad tune the per-day
//     K-line fetch in service.go. They have safe defaults; only
//     override in tests or when the upstream source is slow.
//   - StartupBackfillTimeout caps the §9.5 startup backfill goroutine
//     so a stuck datasource doesn't pin a context forever after a
//     server restart. Defaults to 5m; raise it when KlineLookback × N
//     recs is large.
type PerfConfig struct {
	Enabled                bool          `mapstructure:"enabled"`
	BackfillCron           string        `mapstructure:"backfill_cron"`
	KlineLookback          time.Duration `mapstructure:"kline_lookback"`
	KlineTimeout           time.Duration `mapstructure:"kline_timeout"`
	KlineFuturePad         time.Duration `mapstructure:"kline_future_pad"`
	StartupBackfillTimeout time.Duration `mapstructure:"startup_backfill_timeout"`
}

// AIConfig configures the §11 AI assistant (conversational strategy
// editing). Provider "mock" needs no credentials and is rule-based
// (ships working out of the box); "openai" speaks the OpenAI-compatible
// chat-completions protocol (works with OpenAI, DeepSeek, 通义, Kimi,
// etc. — set base_url + api_key + model accordingly).
type AIConfig struct {
	Enabled  bool          `mapstructure:"enabled"`
	Provider string        `mapstructure:"provider"` // mock | openai
	BaseURL  string        `mapstructure:"base_url"` // e.g. https://api.openai.com/v1
	APIKey   string        `mapstructure:"api_key"`
	Model    string        `mapstructure:"model"` // e.g. gpt-4o-mini, deepseek-chat
	Timeout  time.Duration `mapstructure:"timeout"`
}

// ReviewConfig configures §14.9 automated daily/weekly review reports.
// Empty cron strings disable that cadence; the manual POST
// /api/v1/reviews/generate endpoint still works. Reuses the AI client.
type ReviewConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	DailyCron  string `mapstructure:"daily_cron"`  // e.g. "30 15 * * 1-5"
	WeeklyCron string `mapstructure:"weekly_cron"` // e.g. "0 20 * * 0"
	Timezone   string `mapstructure:"timezone"`
}

// AuthConfig configures §12 authentication. When Enabled, /api/v1 (bar
// login + health) requires a Bearer JWT. JWTSecret signs tokens (a
// random one is generated + logged if empty). AdminUser/AdminPassword
// seed the first admin on an empty DB (random password logged once if
// AdminPassword is empty).
type AuthConfig struct {
	Enabled       bool          `mapstructure:"enabled"`
	JWTSecret     string        `mapstructure:"jwt_secret"`
	AdminUser     string        `mapstructure:"admin_user"`
	AdminPassword string        `mapstructure:"admin_password"`
	TokenTTL      time.Duration `mapstructure:"token_ttl"`
}

type LogConfig struct {
	Level    string `mapstructure:"level"`
	Encoding string `mapstructure:"encoding"`
	Output   string `mapstructure:"output"`
	FilePath string `mapstructure:"file_path"`
}

// Loader is a small wrapper around viper to make testing straightforward.
type Loader struct {
	v *viper.Viper
}

// NewLoader builds a Loader with the standard search paths.
//   - ./configs/config.yaml
//   - ./config.yaml
//   - /etc/af/config.yaml
//
// envFile is optional; when non-empty and the file exists, it is loaded via godotenv
// (real env vars still take precedence).
func NewLoader(configPath, envFile string) (*Loader, error) {
	v := viper.New()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Defaults
	v.SetDefault("app.name", "af-backend")
	v.SetDefault("app.env", "development")
	v.SetDefault("app.version", "0.1.0")

	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.read_timeout", "30s")
	v.SetDefault("server.write_timeout", "30s")
	v.SetDefault("server.shutdown_timeout", "15s")
	v.SetDefault("server.api_base_path", "/api/v1")

	v.SetDefault("db.driver", "mysql")
	v.SetDefault("db.host", "127.0.0.1")
	v.SetDefault("db.port", 3306)
	v.SetDefault("db.user", "root")
	v.SetDefault("db.password", "")
	v.SetDefault("db.name", "astock_selector")
	v.SetDefault("db.charset", "utf8mb4")
	v.SetDefault("db.parse_time", true)
	v.SetDefault("db.loc", "Local")
	v.SetDefault("db.max_open_conns", 50)
	v.SetDefault("db.max_idle_conns", 10)
	v.SetDefault("db.conn_max_lifetime", "1h")
	v.SetDefault("db.log_level", "info")

	v.SetDefault("redis.host", "127.0.0.1")
	v.SetDefault("redis.port", 6379)
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.pool_size", 20)
	v.SetDefault("redis.min_idle_conns", 5)

	v.SetDefault("datasource.provider", "akshare")
	v.SetDefault("datasource.token", "")
	v.SetDefault("datasource.timeout", "10s")
	v.SetDefault("datasource.retry", 3)
	v.SetDefault("datasource.cache_ttl", "5m")
	v.SetDefault("datasource.rate_limit", 60)

	// A3 multi-source defaults. The actual enabled/disabled
	// list is in the YAML, not in defaults, but we set safe
	// fallbacks for the breaker / cache knobs.
	v.SetDefault("datasource.fail_threshold", 5)
	v.SetDefault("datasource.fail_window", "5m")
	v.SetDefault("datasource.cooldown", "10m")
	v.SetDefault("datasource.consistency_threshold", 0.005)

	v.SetDefault("notify.channels", []string{"email"})
	v.SetDefault("notify.default_channel", "email")

	// A4 multi-channel notification defaults.
	v.SetDefault("notify.channel.feishu.enabled", false)
	v.SetDefault("notify.channel.dingtalk.enabled", false)
	v.SetDefault("notify.channel.wecom.enabled", false)
	v.SetDefault("notify.channel.feishu.timeout", "5s")
	v.SetDefault("notify.channel.dingtalk.timeout", "5s")
	v.SetDefault("notify.channel.wecom.timeout", "5s")
	v.SetDefault("notify.channel.primary", "feishu")
	v.SetDefault("notify.channel.retry_attempts", 3)
	v.SetDefault("notify.channel.circuit_breaker.failure_threshold", 5)
	v.SetDefault("notify.channel.circuit_breaker.window", "5m")
	v.SetDefault("notify.channel.circuit_breaker.cooldown", "10m")

	v.SetDefault("cron.enabled", true)
	v.SetDefault("cron.timezone", "Asia/Shanghai")
	v.SetDefault("cron.daily_selection", "0 15 * * 1-5")

	// A7-BE2 executor defaults.
	v.SetDefault("executor.max_concurrency", 4)
	v.SetDefault("executor.default_run_timeout", "5m")
	v.SetDefault("executor.scheduler_enabled", true)
	v.SetDefault("executor.sse_heartbeat", "20s")
	v.SetDefault("executor.sse_max_buffer", 256)

	// A7-BE2 trading calendar defaults (Asia/Shanghai A-share).
	v.SetDefault("calendar.timezone", "Asia/Shanghai")
	v.SetDefault("calendar.morning_start", "09:30")
	v.SetDefault("calendar.morning_end", "11:30")
	v.SetDefault("calendar.afternoon_start", "13:00")
	v.SetDefault("calendar.afternoon_end", "15:00")
	v.SetDefault("calendar.review_start", "15:00")
	v.SetDefault("calendar.review_end", "17:00")

	// §9 perf engine defaults. Routes are on by default; the
	// end-of-day backfill cron is enabled Mon-Fri at 15:30, just
	// after the A-share close. The startup-backfill timeout bounds
	// the §9.5 goroutine so a slow datasource doesn't pin a context
	// forever after a cold start.
	v.SetDefault("perf.enabled", true)
	v.SetDefault("perf.backfill_cron", "30 15 * * 1-5")
	v.SetDefault("perf.kline_lookback", "168h")
	v.SetDefault("perf.kline_timeout", "5s")
	v.SetDefault("perf.kline_future_pad", "24h")
	v.SetDefault("perf.startup_backfill_timeout", "5m")
	v.SetDefault("ai.enabled", true)
	v.SetDefault("ai.provider", "mock")
	v.SetDefault("ai.base_url", "")
	v.SetDefault("ai.api_key", "")
	v.SetDefault("ai.model", "")
	v.SetDefault("ai.timeout", "30s")
	v.SetDefault("review.enabled", true)
	v.SetDefault("review.daily_cron", "30 15 * * 1-5")
	v.SetDefault("review.weekly_cron", "0 20 * * 0")
	v.SetDefault("review.timezone", "Asia/Shanghai")
	v.SetDefault("auth.enabled", false)
	v.SetDefault("auth.jwt_secret", "")
	v.SetDefault("auth.admin_user", "admin")
	v.SetDefault("auth.admin_password", "")
	v.SetDefault("auth.token_ttl", "24h")

	v.SetDefault("log.level", "info")
	v.SetDefault("log.encoding", "console")
	v.SetDefault("log.output", "stdout")
	v.SetDefault("log.file_path", "")

	// Optional .env file
	if envFile != "" {
		if err := godotenv.Load(envFile); err != nil {
			// Only log; missing .env is non-fatal in production where real env vars are set.
			_ = err
		}
	} else {
		// Try default locations silently.
		_ = godotenv.Load()
	}

	// Config file
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath("./configs")
		v.AddConfigPath(".")
		v.AddConfigPath("/etc/af")
	}
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
		// Config file not found is allowed when env vars are sufficient.
	}

	// Bind env vars explicitly for fields that may be set as top-level env vars
	// (e.g. SERVER_PORT, DB_HOST). Map them into the nested keys.
	bindEnv(v)

	return &Loader{v: v}, nil
}

func bindEnv(v *viper.Viper) {
	envBindings := map[string]string{
		"APP_ENV":                             "app.env",
		"APP_NAME":                            "app.name",
		"APP_VERSION":                         "app.version",
		"SERVER_HOST":                         "server.host",
		"SERVER_PORT":                         "server.port",
		"SERVER_READ_TIMEOUT":                 "server.read_timeout",
		"SERVER_WRITE_TIMEOUT":                "server.write_timeout",
		"SERVER_SHUTDOWN_TIMEOUT":             "server.shutdown_timeout",
		"API_BASE_PATH":                       "server.api_base_path",
		"DB_DRIVER":                           "db.driver",
		"DB_HOST":                             "db.host",
		"DB_PORT":                             "db.port",
		"DB_USER":                             "db.user",
		"DB_PASSWORD":                         "db.password",
		"DB_NAME":                             "db.name",
		"DB_CHARSET":                          "db.charset",
		"DB_SSLMODE":                          "db.sslmode",
		"DB_PARSE_TIME":                       "db.parse_time",
		"DB_LOC":                              "db.loc",
		"DB_MAX_OPEN_CONNS":                   "db.max_open_conns",
		"DB_MAX_IDLE_CONNS":                   "db.max_idle_conns",
		"DB_CONN_MAX_LIFETIME":                "db.conn_max_lifetime",
		"DB_LOG_LEVEL":                        "db.log_level",
		"REDIS_HOST":                          "redis.host",
		"REDIS_PORT":                          "redis.port",
		"REDIS_PASSWORD":                      "redis.password",
		"REDIS_DB":                            "redis.db",
		"REDIS_POOL_SIZE":                     "redis.pool_size",
		"REDIS_MIN_IDLE_CONNS":                "redis.min_idle_conns",
		"DATASOURCE_PROVIDER":                 "datasource.provider",
		"DATASOURCE_TOKEN":                    "datasource.token",
		"DATASOURCE_TIMEOUT":                  "datasource.timeout",
		"DATASOURCE_RETRY":                    "datasource.retry",
		"DATASOURCE_CACHE_TTL":                "datasource.cache_ttl",
		"DATASOURCE_RATE_LIMIT":               "datasource.rate_limit",
		"DATASOURCE_FAIL_THRESHOLD":           "datasource.fail_threshold",
		"DATASOURCE_FAIL_WINDOW":              "datasource.fail_window",
		"DATASOURCE_COOLDOWN":                 "datasource.cooldown",
		"DATASOURCE_CONSISTENCY_THRESHOLD":    "datasource.consistency_threshold",
		"NOTIFY_CHANNELS":                     "notify.channels",
		"NOTIFY_DEFAULT_CHANNEL":              "notify.default_channel",
		"NOTIFY_EMAIL_SMTP_HOST":              "notify.email.smtp_host",
		"NOTIFY_EMAIL_SMTP_PORT":              "notify.email.smtp_port",
		"NOTIFY_EMAIL_USERNAME":               "notify.email.username",
		"NOTIFY_EMAIL_PASSWORD":               "notify.email.password",
		"NOTIFY_EMAIL_FROM":                   "notify.email.from",
		"NOTIFY_WECHAT_CORPID":                "notify.wechat.corpid",
		"NOTIFY_WECHAT_AGENTID":               "notify.wechat.agentid",
		"NOTIFY_WECHAT_CORPSECRET":            "notify.wechat.corpsecret",
		"NOTIFY_FEISHU_WEBHOOK":               "notify.feishu.webhook",
		"NOTIFY_CHANNEL_FEISHU_WEBHOOK_URL":   "notify.channel.feishu.webhook_url",
		"NOTIFY_CHANNEL_FEISHU_SECRET":        "notify.channel.feishu.secret",
		"NOTIFY_CHANNEL_FEISHU_ENABLED":       "notify.channel.feishu.enabled",
		"NOTIFY_CHANNEL_FEISHU_TIMEOUT":       "notify.channel.feishu.timeout",
		"NOTIFY_CHANNEL_DINGTALK_WEBHOOK_URL": "notify.channel.dingtalk.webhook_url",
		"NOTIFY_CHANNEL_DINGTALK_SECRET":      "notify.channel.dingtalk.secret",
		"NOTIFY_CHANNEL_DINGTALK_ENABLED":     "notify.channel.dingtalk.enabled",
		"NOTIFY_CHANNEL_DINGTALK_TIMEOUT":     "notify.channel.dingtalk.timeout",
		"NOTIFY_CHANNEL_WECOM_WEBHOOK_URL":    "notify.channel.wecom.webhook_url",
		"NOTIFY_CHANNEL_WECOM_ENABLED":        "notify.channel.wecom.enabled",
		"NOTIFY_CHANNEL_WECOM_TIMEOUT":        "notify.channel.wecom.timeout",
		"NOTIFY_CHANNEL_PRIMARY":              "notify.channel.primary",
		"NOTIFY_CHANNEL_RETRY_ATTEMPTS":       "notify.channel.retry_attempts",
		"NOTIFY_CHANNEL_CB_FAILURE_THRESHOLD": "notify.channel.circuit_breaker.failure_threshold",
		"NOTIFY_CHANNEL_CB_WINDOW":            "notify.channel.circuit_breaker.window",
		"NOTIFY_CHANNEL_CB_COOLDOWN":          "notify.channel.circuit_breaker.cooldown",
		"CRON_ENABLED":                        "cron.enabled",
		"CRON_TIMEZONE":                       "cron.timezone",
		"CRON_DAILY_SELECTION":                "cron.daily_selection",
		"EXECUTOR_MAX_CONCURRENCY":            "executor.max_concurrency",
		"EXECUTOR_DEFAULT_RUN_TIMEOUT":        "executor.default_run_timeout",
		"EXECUTOR_SCHEDULER_ENABLED":          "executor.scheduler_enabled",
		"EXECUTOR_SSE_HEARTBEAT":              "executor.sse_heartbeat",
		"EXECUTOR_SSE_MAX_BUFFER":             "executor.sse_max_buffer",
		"CALENDAR_TIMEZONE":                   "calendar.timezone",
		"CALENDAR_MORNING_START":              "calendar.morning_start",
		"CALENDAR_MORNING_END":                "calendar.morning_end",
		"CALENDAR_AFTERNOON_START":            "calendar.afternoon_start",
		"CALENDAR_AFTERNOON_END":              "calendar.afternoon_end",
		"CALENDAR_REVIEW_START":               "calendar.review_start",
		"CALENDAR_REVIEW_END":                 "calendar.review_end",
		"PERF_ENABLED":                        "perf.enabled",
		"AI_ENABLED":                          "ai.enabled",
		"AI_PROVIDER":                         "ai.provider",
		"AI_BASE_URL":                         "ai.base_url",
		"AI_API_KEY":                          "ai.api_key",
		"AI_MODEL":                            "ai.model",
		"AI_TIMEOUT":                          "ai.timeout",
		"AUTH_ENABLED":                        "auth.enabled",
		"AUTH_JWT_SECRET":                     "auth.jwt_secret",
		"AUTH_ADMIN_USER":                     "auth.admin_user",
		"AUTH_ADMIN_PASSWORD":                 "auth.admin_password",
		"AUTH_TOKEN_TTL":                      "auth.token_ttl",
		"PERF_BACKFILL_CRON":                  "perf.backfill_cron",
		"PERF_KLINE_LOOKBACK":                 "perf.kline_lookback",
		"PERF_KLINE_TIMEOUT":                  "perf.kline_timeout",
		"PERF_KLINE_FUTURE_PAD":               "perf.kline_future_pad",
		"PERF_STARTUP_BACKFILL_TIMEOUT":       "perf.startup_backfill_timeout",
		"LOG_LEVEL":                           "log.level",
		"LOG_ENCODING":                        "log.encoding",
		"LOG_OUTPUT":                          "log.output",
		"LOG_FILE_PATH":                       "log.file_path",
	}
	for env, key := range envBindings {
		_ = v.BindEnv(key, env)
	}
}

// Load unmarshals the configuration into a Config struct.
func (l *Loader) Load() (*Config, error) {
	var c Config
	if err := l.v.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate performs basic sanity checks. It does NOT require DB/Redis to be reachable.
func (c *Config) Validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server.port: %d", c.Server.Port)
	}
	if c.App.Env == "" {
		c.App.Env = "development"
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Log.Encoding == "" {
		c.Log.Encoding = "console"
	}
	return nil
}

// IsDev reports whether the app is running in development mode.
func (c *Config) IsDev() bool {
	return strings.EqualFold(c.App.Env, "development")
}

// DSN returns a GORM-compatible MySQL DSN.
func (d *DBConfig) DSN() string {
	parseTime := "False"
	if d.ParseTime {
		parseTime = "True"
	}
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=%s&loc=%s",
		d.User, d.Password, d.Host, d.Port, d.Name, d.Charset, parseTime, d.Loc,
	)
}

// PostgresDSN builds a lib/pq-style keyword DSN for the postgres driver.
// SSLMode defaults to "disable" (typical for a self-hosted box on a
// trusted network); set DB_SSLMODE=require (or verify-full) when the
// DB is remote. TimeZone takes DBConfig.Loc only when it's a real IANA
// zone; the MySQL-driver default "Local" is meaningless to Postgres
// (FATAL: invalid value for parameter "TimeZone": "Local"), so it — and
// empty — fall back to UTC.
func (d *DBConfig) PostgresDSN() string {
	sslMode := d.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}
	tz := d.Loc
	if tz == "" || tz == "Local" {
		tz = "UTC"
	}
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		d.Host, d.Port, d.User, d.Password, d.Name, sslMode, tz,
	)
}
