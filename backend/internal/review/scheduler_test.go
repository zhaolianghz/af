// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package review

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScheduler_NilService(t *testing.T) {
	_, err := NewScheduler(nil, SchedulerConfig{})
	require.Error(t, err)
}

func TestScheduler_BadTimezone(t *testing.T) {
	svc := NewService(newDB(t), nil)
	_, err := NewScheduler(svc, SchedulerConfig{Timezone: "Not/AZone"})
	require.Error(t, err)
}

func TestScheduler_DefaultTimezone(t *testing.T) {
	svc := NewService(newDB(t), nil)
	s, err := NewScheduler(svc, SchedulerConfig{}) // empty tz → Asia/Shanghai
	require.NoError(t, err)
	require.NotNil(t, s)
}

func TestScheduler_BadCronRejectedOnStart(t *testing.T) {
	svc := NewService(newDB(t), nil)
	s, err := NewScheduler(svc, SchedulerConfig{DailyCron: "not a cron"})
	require.NoError(t, err)
	require.Error(t, s.Start(), "invalid cron expr must fail Start")
}

func TestScheduler_EmptyCronsStartStop(t *testing.T) {
	// Both crons empty → Start is a no-op success; Stop is clean.
	svc := NewService(newDB(t), nil)
	s, err := NewScheduler(svc, SchedulerConfig{})
	require.NoError(t, err)
	require.NoError(t, s.Start())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, s.Stop(ctx))
	// Stop again is idempotent (not active).
	require.NoError(t, s.Stop(ctx))
}

func TestScheduler_ValidCronsStartStop(t *testing.T) {
	svc := NewService(newDB(t), nil)
	s, err := NewScheduler(svc, SchedulerConfig{
		DailyCron:  "30 15 * * 1-5",
		WeeklyCron: "0 20 * * 0",
		Timezone:   "Asia/Shanghai",
	})
	require.NoError(t, err)
	require.NoError(t, s.Start())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, s.Stop(ctx))
}
