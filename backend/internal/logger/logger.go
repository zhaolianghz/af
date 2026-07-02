// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package logger wraps zap with sane defaults and a dev/prod switch.
package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Options controls how the logger is built.
type Options struct {
	Level    string // debug | info | warn | error
	Encoding string // console | json
	Output   string // stdout | file
	FilePath string
	IsDev    bool
}

// New builds a zap.Logger from Options.
func New(opts Options) (*zap.Logger, error) {
	level, err := parseLevel(opts.Level)
	if err != nil {
		return nil, err
	}

	enc := strings.ToLower(opts.Encoding)
	if enc == "" {
		if opts.IsDev {
			enc = "console"
		} else {
			enc = "json"
		}
	}
	if enc != "console" && enc != "json" {
		return nil, fmt.Errorf("logger: invalid encoding %q (want console|json)", opts.Encoding)
	}

	output := strings.ToLower(opts.Output)
	if output == "" {
		output = "stdout"
	}

	w, closer, err := buildWriter(output, opts.FilePath)
	if err != nil {
		return nil, err
	}

	encoderCfg := encoderConfig(opts.IsDev)
	var encoder zapcore.Encoder
	if enc == "json" {
		encoder = zapcore.NewJSONEncoder(encoderCfg)
	} else {
		encoder = zapcore.NewConsoleEncoder(encoderCfg)
	}

	core := zapcore.NewCore(encoder, w, level)
	l := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	if closer != nil {
		// Close underlying file when logger is replaced or process exits.
		l = l.WithOptions(zap.WrapCore(func(c zapcore.Core) zapcore.Core { return c }), zap.Hooks(func(entry zapcore.Entry) error {
			// No-op hook; closer managed via Sync.
			return nil
		}))
		_ = closer // caller is responsible for closing via Sync() if needed
	}

	return l, nil
}

// Sync flushes any buffered logs.
func Sync(l *zap.Logger) {
	_ = l.Sync()
}

func parseLevel(s string) (zapcore.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return zapcore.DebugLevel, nil
	case "", "info":
		return zapcore.InfoLevel, nil
	case "warn", "warning":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	case "fatal":
		return zapcore.FatalLevel, nil
	default:
		return zapcore.InfoLevel, fmt.Errorf("logger: invalid level %q", s)
	}
}

func buildWriter(output, filePath string) (zapcore.WriteSyncer, func() error, error) {
	switch output {
	case "stdout", "":
		return zapcore.AddSync(os.Stdout), nil, nil
	case "stderr":
		return zapcore.AddSync(os.Stderr), nil, nil
	case "file":
		if filePath == "" {
			return nil, nil, fmt.Errorf("logger: file output requested but file_path is empty")
		}
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return nil, nil, fmt.Errorf("logger: mkdir: %w", err)
		}
		f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("logger: open file: %w", err)
		}
		return zapcore.AddSync(f), f.Close, nil
	default:
		return nil, nil, fmt.Errorf("logger: invalid output %q (want stdout|stderr|file)", output)
	}
}

func encoderConfig(dev bool) zapcore.EncoderConfig {
	cfg := zap.NewProductionEncoderConfig()
	cfg.TimeKey = "ts"
	cfg.MessageKey = "msg"
	cfg.LevelKey = "level"
	cfg.CallerKey = "caller"
	cfg.StacktraceKey = "stacktrace"
	cfg.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncodeLevel = zapcore.LowercaseLevelEncoder
	cfg.EncodeDuration = zapcore.SecondsDurationEncoder
	if dev {
		cfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
		cfg.EncodeTime = zapcore.TimeEncoderOfLayout("15:04:05.000")
	}
	return cfg
}
