// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package notify

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// HealthSnapshot is the JSON shape returned by GET /api/v1/notify/health.
type HealthSnapshot struct {
	Channels map[string]ChannelHealth `json:"channels"`
	Healthy  bool                     `json:"healthy"`
}

// ChannelHealth describes a single channel's status.
type ChannelHealth struct {
	Registered bool   `json:"registered"`
	State      string `json:"state"`
	Healthy    bool   `json:"healthy"`
}

// BuildHealth walks breakers and produces a HealthSnapshot. Channels
// not in breakers (i.e. not configured) are reported with
// Registered=false.
func BuildHealth(breakers map[string]*CircuitBreaker) HealthSnapshot {
	snap := HealthSnapshot{
		Channels: map[string]ChannelHealth{},
		Healthy:  true,
	}
	for _, name := range []string{"feishu", "dingtalk", "wecom"} {
		br, ok := breakers[name]
		if !ok {
			snap.Channels[name] = ChannelHealth{Registered: false, State: "unconfigured", Healthy: false}
			snap.Healthy = false
			continue
		}
		state := br.State().String()
		healthy := br.IsHealthy()
		snap.Channels[name] = ChannelHealth{
			Registered: true,
			State:      state,
			Healthy:    healthy,
		}
		if !healthy {
			snap.Healthy = false
		}
	}
	return snap
}

// NewHealthHandler returns a Gin handler for GET /api/v1/notify/health.
// It is safe to mount even when breakers is nil or empty.
func NewHealthHandler(breakers map[string]*CircuitBreaker) gin.HandlerFunc {
	return func(c *gin.Context) {
		snap := BuildHealth(breakers)
		c.JSON(http.StatusOK, snap)
	}
}

// NewTestPingHandler returns a Gin handler for POST /api/v1/notify/test.
// It sends a small "ping" Message through the manager. The handler
// returns 200 on success and 502 when all channels fail.
func NewTestPingHandler(mgr Manager, logger *zap.Logger) (gin.HandlerFunc, error) {
	if mgr == nil {
		return nil, fmt.Errorf("notify: nil manager")
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return func(c *gin.Context) {
		msg := &Message{
			Title:     "ping",
			Content:   "af-notify ping",
			Type:      MsgAlert,
			Timestamp: time.Now(),
		}
		err := mgr.Send(c.Request.Context(), msg)
		if err != nil {
			logger.Warn("notify test send failed", zap.Error(err))
			c.JSON(http.StatusBadGateway, gin.H{
				"ok":    false,
				"error": err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}, nil
}
