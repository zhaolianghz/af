// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package logger

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestNewJSONLogger(t *testing.T) {
	var buf bytes.Buffer
	// We test the encoder path directly using a custom core to avoid file IO.
	cfg := encoderConfig(false)
	enc := zapcore.NewJSONEncoder(cfg)
	core := zapcore.NewCore(enc, zapcore.AddSync(&buf), zapcore.InfoLevel)
	l := zap.New(core)

	l.Info("hello", zap.String("k", "v"), zap.Int("n", 42))
	Sync(l)

	var out map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out))
	assert.Equal(t, "hello", out["msg"])
	assert.Equal(t, "info", out["level"])
	assert.Equal(t, "v", out["k"])
	assert.Equal(t, float64(42), out["n"])
}

func TestNewConsoleLogger(t *testing.T) {
	var buf bytes.Buffer
	cfg := encoderConfig(true)
	enc := zapcore.NewConsoleEncoder(cfg)
	core := zapcore.NewCore(enc, zapcore.AddSync(&buf), zapcore.DebugLevel)
	l := zap.New(core)

	l.Debug("dbg")
	Sync(l)

	out := buf.String()
	assert.Contains(t, out, "dbg")
	assert.Contains(t, out, "DEBUG")
}

func TestNewRespectsOptions(t *testing.T) {
	l, err := New(Options{Level: "debug", Encoding: "json", Output: "stdout", IsDev: false})
	require.NoError(t, err)
	require.NotNil(t, l)
	defer Sync(l)
}

func TestNewInvalidLevel(t *testing.T) {
	_, err := New(Options{Level: "loud"})
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "level"))
}

func TestNewInvalidEncoding(t *testing.T) {
	_, err := New(Options{Encoding: "yaml"})
	assert.Error(t, err)
}

func TestNewFileRequiresPath(t *testing.T) {
	_, err := New(Options{Output: "file"})
	assert.Error(t, err)
}

func TestNewFilePath(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/app.log"
	l, err := New(Options{Output: "file", FilePath: path, Encoding: "json"})
	require.NoError(t, err)
	require.NotNil(t, l)
	l.Info("ok", zap.String("who", "tester"))
	Sync(l)
}
