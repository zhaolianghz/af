// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package registry

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/config"
)

func TestNew_NoChannelsEnabled(t *testing.T) {
	mgr, breakers := New(Options{
		Cfg:    config.NotifyChannelConfig{},
		Logger: zap.NewNop(),
	})
	require.NotNil(t, mgr)
	assert.Empty(t, breakers)
	assert.Equal(t, []string{}, mgr.List())
}

func TestNew_OnlyFeishuEnabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"StatusCode":0}`))
	}))
	defer srv.Close()

	mgr, breakers := New(Options{
		Cfg: config.NotifyChannelConfig{
			Primary: "feishu",
			Feishu: config.ChannelConfig{
				Enabled:    true,
				WebhookURL: srv.URL,
				Timeout:    100 * time.Millisecond,
			},
			CircuitBreaker: config.CircuitBreakerConfig{
				FailureThreshold: 5,
				Window:           5 * time.Minute,
				Cooldown:         10 * time.Minute,
			},
		},
		Logger: zap.NewNop(),
	})

	assert.Len(t, breakers, 1)
	assert.Contains(t, breakers, "feishu")
	assert.ElementsMatch(t, []string{"feishu"}, mgr.List())
}

func TestNew_AllThreeEnabled(t *testing.T) {
	makeServer := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`ok`))
		}))
	}
	feishuSrv := makeServer()
	defer feishuSrv.Close()
	dingSrv := makeServer()
	defer dingSrv.Close()
	wecomSrv := makeServer()
	defer wecomSrv.Close()

	mgr, breakers := New(Options{
		Cfg: config.NotifyChannelConfig{
			Primary:  "feishu",
			Fallback: []string{"dingtalk", "wecom"},
			Feishu:   config.ChannelConfig{Enabled: true, WebhookURL: feishuSrv.URL, Timeout: 200 * time.Millisecond},
			DingTalk: config.ChannelConfig{Enabled: true, WebhookURL: dingSrv.URL, Timeout: 200 * time.Millisecond},
			WeCom:    config.ChannelConfig{Enabled: true, WebhookURL: wecomSrv.URL, Timeout: 200 * time.Millisecond},
			CircuitBreaker: config.CircuitBreakerConfig{
				FailureThreshold: 5,
				Window:           5 * time.Minute,
				Cooldown:         10 * time.Minute,
			},
		},
		Logger: zap.NewNop(),
	})

	assert.Len(t, breakers, 3)
	assert.ElementsMatch(t, []string{"feishu", "dingtalk", "wecom"}, mgr.List())
}

func TestNew_DisabledButConfigured(t *testing.T) {
	mgr, breakers := New(Options{
		Cfg: config.NotifyChannelConfig{
			Primary: "feishu",
			// enabled=false → should be skipped even with a URL set.
			Feishu: config.ChannelConfig{Enabled: false, WebhookURL: "http://example.invalid"},
		},
		Logger: zap.NewNop(),
	})
	assert.Empty(t, breakers)
	assert.Empty(t, mgr.List())
}

func TestNew_EmptyURLNotRegistered(t *testing.T) {
	mgr, breakers := New(Options{
		Cfg: config.NotifyChannelConfig{
			Primary: "feishu",
			Feishu: config.ChannelConfig{Enabled: true, WebhookURL: ""}, // empty URL
		},
		Logger: zap.NewNop(),
	})
	assert.Empty(t, breakers)
	assert.Empty(t, mgr.List())
}

func TestTimeoutOr(t *testing.T) {
	assert.Equal(t, time.Second, timeoutOr(time.Second, 5*time.Second))
	assert.Equal(t, 5*time.Second, timeoutOr(0, 5*time.Second))
	assert.Equal(t, 5*time.Second, timeoutOr(-1, 5*time.Second))
}
