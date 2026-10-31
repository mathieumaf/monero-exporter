package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	defaultLogLevel  = "info"
	logFormatJSON    = "json"
	logFormatConsole = "console"

	// defaultRPCTimeout is the per-call budget passed to the go-monero HTTP
	// client. A stuck RPC (for example get_peer_list) fails on its own
	// instead of consuming the whole collection cycle.
	defaultRPCTimeout = 30 * time.Second
)

var errUnsupportedLogFormat = errors.New("unsupported log format")

func newLogger(level, format string) (*zap.Logger, error) {
	cfg, err := loggerConfig(level, format)
	if err != nil {
		return nil, err
	}

	logger, err := cfg.Build()
	if err != nil {
		return nil, fmt.Errorf("build logger: %w", err)
	}

	return logger, nil
}

func loggerConfig(level, format string) (zap.Config, error) {
	var lvl zapcore.Level
	if err := lvl.UnmarshalText([]byte(strings.TrimSpace(level))); err != nil {
		return zap.Config{}, fmt.Errorf("parse log level %q: %w", level, err)
	}

	encoding, err := logEncoding(format)
	if err != nil {
		return zap.Config{}, err
	}

	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(lvl)
	cfg.Encoding = encoding

	return cfg, nil
}

func logEncoding(format string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case logFormatJSON:
		return logFormatJSON, nil
	case logFormatConsole:
		return logFormatConsole, nil
	default:
		return "", fmt.Errorf("log format %q: %w", format, errUnsupportedLogFormat)
	}
}
