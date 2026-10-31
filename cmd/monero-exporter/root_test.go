package main

import (
	"context"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/mathieumaf/monero-exporter/pkg/collector"
)

func TestFlagDefaults(t *testing.T) {
	t.Parallel()

	c := &command{}
	_ = c.Cmd()

	if c.rpcTimeout != 30*time.Second {
		t.Fatalf("rpc timeout = %s, want 30s", c.rpcTimeout)
	}

	if c.collectTimeout != 2*time.Minute {
		t.Fatalf("collect timeout = %s, want 2m", c.collectTimeout)
	}

	if c.collectTimeout != collector.DefaultCollectTimeout {
		t.Fatalf("collect timeout = %s, want collector default %s", c.collectTimeout, collector.DefaultCollectTimeout)
	}

	if c.logLevel != defaultLogLevel {
		t.Fatalf("log level = %q, want %q", c.logLevel, defaultLogLevel)
	}

	if c.logFormat != logFormatJSON {
		t.Fatalf("log format = %q, want %q", c.logFormat, logFormatJSON)
	}
}

func TestFlagOverrides(t *testing.T) {
	t.Parallel()

	c := &command{}
	cmd := c.Cmd()
	cmd.SetArgs([]string{
		"--rpc-timeout=15s",
		"--collect-timeout=45s",
		"--log.level=warn",
		"--log.format=console",
	})
	cmd.RunE = func(*cobra.Command, []string) error { return nil }

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if c.rpcTimeout != 15*time.Second {
		t.Fatalf("rpc timeout = %s", c.rpcTimeout)
	}

	if c.collectTimeout != 45*time.Second {
		t.Fatalf("collect timeout = %s", c.collectTimeout)
	}

	if c.logLevel != "warn" || c.logFormat != logFormatConsole {
		t.Fatalf("log config = %s %s", c.logLevel, c.logFormat)
	}
}

func TestValidateTimeouts(t *testing.T) {
	t.Parallel()

	valid := &command{rpcTimeout: time.Second, collectTimeout: time.Minute}
	if err := valid.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	cases := []*command{
		{rpcTimeout: 0, collectTimeout: time.Second},
		{rpcTimeout: -time.Second, collectTimeout: time.Second},
		{rpcTimeout: time.Second, collectTimeout: 0},
	}

	for _, c := range cases {
		if err := c.validate(); err == nil {
			t.Fatalf("validate(%+v) succeeded", c)
		}
	}
}

func TestRunERejectsBadConfig(t *testing.T) {
	t.Parallel()

	badLevel := &command{
		rpcTimeout:     time.Second,
		collectTimeout: time.Minute,
		logLevel:       "verbose",
		logFormat:      logFormatJSON,
	}
	if err := badLevel.RunE(nil, nil); err == nil {
		t.Fatal("expected log level error")
	}

	badTimeout := &command{
		rpcTimeout:     0,
		collectTimeout: time.Minute,
		logLevel:       defaultLogLevel,
		logFormat:      logFormatJSON,
	}
	if err := badTimeout.RunE(nil, nil); err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestRPCTimeoutWarningLogged(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zapcore.WarnLevel)
	logger := zap.New(core)

	c := &command{rpcTimeout: 2 * time.Minute, collectTimeout: time.Minute}
	c.warnTimeouts(logger)

	if logs.Len() != 1 {
		t.Fatalf("warnings = %d, want 1", logs.Len())
	}

	c.collectTimeout = 2 * time.Minute
	c.warnTimeouts(logger)

	if logs.Len() != 1 {
		t.Fatalf("warnings = %d, want no extra warning", logs.Len())
	}
}

func TestCommandContext(t *testing.T) {
	t.Parallel()

	if commandContext(nil) == nil {
		t.Fatal("nil command context")
	}

	cmd := (&command{}).Cmd()
	if commandContext(cmd) == nil {
		t.Fatal("missing command context")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd.SetContext(ctx)

	if commandContext(cmd) != ctx {
		t.Fatal("command context was not preserved")
	}
}

func TestLoggerConfig(t *testing.T) {
	t.Parallel()

	cfg, err := loggerConfig("warn", "console")
	if err != nil {
		t.Fatalf("logger config: %v", err)
	}

	if cfg.Encoding != logFormatConsole {
		t.Fatalf("encoding = %s", cfg.Encoding)
	}

	if !cfg.Level.Enabled(zapcore.WarnLevel) || cfg.Level.Enabled(zapcore.InfoLevel) {
		t.Fatalf("level = %s", cfg.Level.Level())
	}

	if _, err := loggerConfig("info", "yaml"); err == nil {
		t.Fatal("expected unsupported format error")
	}

	if _, err := newLogger("verbose", logFormatJSON); err == nil {
		t.Fatal("expected unsupported level error")
	}

	logger, err := newLogger("debug", logFormatJSON)
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	_ = logger.Sync()
}
