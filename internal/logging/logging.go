// Package logging provides structured JSON logging with sanitization.
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// sensitiveKeys are keys that should be sanitized in logs.
var sensitiveKeys = []string{
	"password",
	"secret",
	"token",
	"key",
	"credential",
	"passphrase",
	"auth",
}

// SanitizingHandler wraps a slog.Handler to sanitize sensitive data.
type SanitizingHandler struct {
	handler  slog.Handler
	sanitize bool
}

// NewSanitizingHandler creates a new sanitizing handler.
func NewSanitizingHandler(handler slog.Handler, sanitize bool) *SanitizingHandler {
	return &SanitizingHandler{
		handler:  handler,
		sanitize: sanitize,
	}
}

// Enabled implements slog.Handler.
func (h *SanitizingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

// Handle implements slog.Handler.
func (h *SanitizingHandler) Handle(ctx context.Context, r slog.Record) error {
	if !h.sanitize {
		return h.handler.Handle(ctx, r)
	}

	// Create a new record with sanitized attributes
	newRecord := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		newRecord.AddAttrs(h.sanitizeAttr(a))
		return true
	})

	return h.handler.Handle(ctx, newRecord)
}

// WithAttrs implements slog.Handler.
func (h *SanitizingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if h.sanitize {
		sanitized := make([]slog.Attr, len(attrs))
		for i, a := range attrs {
			sanitized[i] = h.sanitizeAttr(a)
		}
		attrs = sanitized
	}
	return &SanitizingHandler{
		handler:  h.handler.WithAttrs(attrs),
		sanitize: h.sanitize,
	}
}

// WithGroup implements slog.Handler.
func (h *SanitizingHandler) WithGroup(name string) slog.Handler {
	return &SanitizingHandler{
		handler:  h.handler.WithGroup(name),
		sanitize: h.sanitize,
	}
}

// sanitizeAttr sanitizes an attribute if its key matches a sensitive key.
func (h *SanitizingHandler) sanitizeAttr(a slog.Attr) slog.Attr {
	key := strings.ToLower(a.Key)
	for _, sensitive := range sensitiveKeys {
		if strings.Contains(key, sensitive) {
			return slog.String(a.Key, "[REDACTED]")
		}
	}

	// Recursively sanitize group attributes
	if a.Value.Kind() == slog.KindGroup {
		attrs := a.Value.Group()
		sanitized := make([]slog.Attr, len(attrs))
		for i, attr := range attrs {
			sanitized[i] = h.sanitizeAttr(attr)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(sanitized...)}
	}

	return a
}

// Setup initializes the global logger with the given level and sanitization setting.
func Setup(level string, sanitize bool) {
	var logLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	jsonHandler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	})

	handler := NewSanitizingHandler(jsonHandler, sanitize)
	logger := slog.New(handler)
	slog.SetDefault(logger)
}

// DebugPTYRead logs PTY read data in debug mode.
func DebugPTYRead(sessionID string, data []byte, n int) {
	if n > 0 {
		slog.Debug("pty read",
			slog.String("session_id", sessionID),
			slog.Int("bytes", n),
			slog.String("data", truncateForLog(string(data[:n]), 200)),
			slog.String("hex", hexDump(data[:n], 64)),
		)
	}
}

// DebugPTYWrite logs PTY write data in debug mode.
func DebugPTYWrite(sessionID string, data string) {
	slog.Debug("pty write",
		slog.String("session_id", sessionID),
		slog.Int("bytes", len(data)),
		slog.String("data", truncateForLog(data, 200)),
		slog.String("hex", hexDump([]byte(data), 64)),
	)
}

// DebugPromptDetection logs prompt detection attempts.
func DebugPromptDetection(sessionID string, buffer string, matched bool, patternName string) {
	slog.Debug("prompt detection",
		slog.String("session_id", sessionID),
		slog.Int("buffer_len", len(buffer)),
		slog.Bool("matched", matched),
		slog.String("pattern", patternName),
		slog.String("buffer_tail", truncateForLog(buffer, 100)),
	)
}

// truncateForLog truncates a string for logging.
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// hexDump returns a hex representation of bytes.
func hexDump(data []byte, maxLen int) string {
	if len(data) > maxLen {
		data = data[:maxLen]
	}

	result := make([]byte, 0, len(data)*3)
	for i, b := range data {
		if i > 0 {
			result = append(result, ' ')
		}
		result = append(result, hexChar(b>>4), hexChar(b&0x0f))
	}
	return string(result)
}

func hexChar(b byte) byte {
	if b < 10 {
		return '0' + b
	}
	return 'a' + b - 10
}
