// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package registry wires notify.Channel implementations into a
// notify.Manager. It lives in its own sub-package to break the
// import cycle: the channel sub-packages import the parent
// "notify" package for the Channel interface, so the parent cannot
// import them back.
//
// The exported New function returns the assembled Manager and a
// per-channel circuit-breaker map so /api/v1/notify/health can
// report breaker state.
package registry

import (
	"time"

	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/config"
	"github.com/skyzhao/af/internal/notify"
	"github.com/skyzhao/af/internal/notify/channel/dingtalk"
	"github.com/skyzhao/af/internal/notify/channel/feishu"
	"github.com/skyzhao/af/internal/notify/channel/wecom"
)

// Options configures New.
type Options struct {
	Cfg    config.NotifyChannelConfig
	Logger *zap.Logger
}

// New assembles a *notify.DefaultManager with the channels enabled in cfg.
// Channels with Enabled=false or an empty WebhookURL are skipped
// (logged at info). The returned breaker map is keyed by channel name
// and is empty when no channel is enabled.
func New(opts Options) (*notify.DefaultManager, map[string]*notify.CircuitBreaker) {
	logger := opts.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	cfg := opts.Cfg
	cbCfg := notify.CircuitConfig{
		FailureThreshold: cfg.CircuitBreaker.FailureThreshold,
		Window:           cfg.CircuitBreaker.Window,
		Cooldown:         cfg.CircuitBreaker.Cooldown,
	}

	breakers := map[string]*notify.CircuitBreaker{}

	manager := notify.NewManager(notify.ManagerOptions{
		Primary:  cfg.Primary,
		Fallback: cfg.Fallback,
		Retry: notify.RetryConfig{
			Attempts:       cfg.RetryAttempts,
			InitialBackoff: time.Second,
		},
		Timeout: 5 * time.Second,
		Logger:  logger,
	})

	if cfg.Feishu.Enabled && cfg.Feishu.WebhookURL != "" {
		br := notify.NewCircuitBreaker(cbCfg)
		breakers[feishu.ChannelName] = br
		manager.RegisterChannel(feishu.ChannelName, feishu.New(feishu.Config{
			WebhookURL: cfg.Feishu.WebhookURL,
			Secret:     cfg.Feishu.Secret,
			Timeout:    timeoutOr(cfg.Feishu.Timeout, 5*time.Second),
		}, br, logger))
	} else {
		logger.Info("notify feishu channel disabled or not configured",
			zap.Bool("enabled", cfg.Feishu.Enabled),
			zap.Bool("webhook_set", cfg.Feishu.WebhookURL != ""),
		)
	}

	if cfg.DingTalk.Enabled && cfg.DingTalk.WebhookURL != "" {
		br := notify.NewCircuitBreaker(cbCfg)
		breakers[dingtalk.ChannelName] = br
		manager.RegisterChannel(dingtalk.ChannelName, dingtalk.New(dingtalk.Config{
			WebhookURL: cfg.DingTalk.WebhookURL,
			Secret:     cfg.DingTalk.Secret,
			Timeout:    timeoutOr(cfg.DingTalk.Timeout, 5*time.Second),
		}, br, logger))
	} else {
		logger.Info("notify dingtalk channel disabled or not configured",
			zap.Bool("enabled", cfg.DingTalk.Enabled),
			zap.Bool("webhook_set", cfg.DingTalk.WebhookURL != ""),
		)
	}

	if cfg.WeCom.Enabled && cfg.WeCom.WebhookURL != "" {
		br := notify.NewCircuitBreaker(cbCfg)
		breakers[wecom.ChannelName] = br
		manager.RegisterChannel(wecom.ChannelName, wecom.New(wecom.Config{
			WebhookURL: cfg.WeCom.WebhookURL,
			Timeout:    timeoutOr(cfg.WeCom.Timeout, 5*time.Second),
		}, br, logger))
	} else {
		logger.Info("notify wecom channel disabled or not configured",
			zap.Bool("enabled", cfg.WeCom.Enabled),
			zap.Bool("webhook_set", cfg.WeCom.WebhookURL != ""),
		)
	}

	if len(breakers) == 0 {
		logger.Warn("notify: no channels configured — notifications will fail",
			zap.String("hint", "set NOTIFY_CHANNEL_<NAME>_ENABLED=true and *_WEBHOOK_URL"),
		)
	}

	return manager, breakers
}

func timeoutOr(d, fallback time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return fallback
}
