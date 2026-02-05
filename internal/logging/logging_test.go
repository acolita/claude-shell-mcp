package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

// ============================================================
// truncateForLog tests
// ============================================================

func TestTruncateForLog_ShortString(t *testing.T) {
	result := truncateForLog("hello", 10)
	if result != "hello" {
		t.Errorf("expected %q, got %q", "hello", result)
	}
}

func TestTruncateForLog_ExactMaxLen(t *testing.T) {
	result := truncateForLog("hello", 5)
	if result != "hello" {
		t.Errorf("expected %q, got %q", "hello", result)
	}
}

func TestTruncateForLog_LongerThanMaxLen(t *testing.T) {
	result := truncateForLog("hello world", 5)
	expected := "hello..."
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestTruncateForLog_EmptyString(t *testing.T) {
	result := truncateForLog("", 10)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestTruncateForLog_MaxLenZero(t *testing.T) {
	result := truncateForLog("hello", 0)
	expected := "..."
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// ============================================================
// hexDump tests
// ============================================================

func TestHexDump_Empty(t *testing.T) {
	result := hexDump([]byte{}, 10)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestHexDump_ShortData(t *testing.T) {
	result := hexDump([]byte{0x48, 0x69}, 10)
	expected := "48 69"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestHexDump_LongerThanMaxLen(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	result := hexDump(data, 3)
	expected := "01 02 03"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestHexDump_SingleByte(t *testing.T) {
	result := hexDump([]byte{0xff}, 10)
	expected := "ff"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestHexDump_AllZeros(t *testing.T) {
	result := hexDump([]byte{0x00, 0x00}, 10)
	expected := "00 00"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// ============================================================
// hexChar tests
// ============================================================

func TestHexChar_Digits(t *testing.T) {
	for i := byte(0); i < 10; i++ {
		expected := '0' + i
		result := hexChar(i)
		if result != expected {
			t.Errorf("hexChar(%d): expected %c, got %c", i, expected, result)
		}
	}
}

func TestHexChar_Letters(t *testing.T) {
	for i := byte(10); i < 16; i++ {
		expected := 'a' + i - 10
		result := hexChar(i)
		if result != expected {
			t.Errorf("hexChar(%d): expected %c, got %c", i, expected, result)
		}
	}
}

// ============================================================
// NewSanitizingHandler tests
// ============================================================

func TestNewSanitizingHandler(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, nil)
	handler := NewSanitizingHandler(inner, true)

	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
	if handler.sanitize != true {
		t.Error("expected sanitize to be true")
	}
	if handler.handler != inner {
		t.Error("expected inner handler to be set")
	}
}

func TestNewSanitizingHandler_SanitizeFalse(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, nil)
	handler := NewSanitizingHandler(inner, false)

	if handler.sanitize != false {
		t.Error("expected sanitize to be false")
	}
}

// ============================================================
// SanitizingHandler.Enabled tests
// ============================================================

func TestSanitizingHandler_Enabled_DelegatesToInner(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	})
	handler := NewSanitizingHandler(inner, true)

	ctx := context.Background()

	// Debug should be disabled since inner handler is at Warn level
	if handler.Enabled(ctx, slog.LevelDebug) {
		t.Error("expected debug to be disabled")
	}

	// Info should be disabled
	if handler.Enabled(ctx, slog.LevelInfo) {
		t.Error("expected info to be disabled")
	}

	// Warn should be enabled
	if !handler.Enabled(ctx, slog.LevelWarn) {
		t.Error("expected warn to be enabled")
	}

	// Error should be enabled
	if !handler.Enabled(ctx, slog.LevelError) {
		t.Error("expected error to be enabled")
	}
}

// ============================================================
// Helper: parse JSON log output
// ============================================================

func parseLogOutput(t *testing.T, buf *bytes.Buffer) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse log output: %v\nraw: %s", err, buf.String())
	}
	return result
}

// ============================================================
// SanitizingHandler.Handle tests
// ============================================================

func TestHandle_SanitizeTrue_RedactsPassword(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)
	logger := slog.New(handler)

	logger.Info("test", slog.String("password", "mysecretpass"))

	result := parseLogOutput(t, &buf)
	if result["password"] != "[REDACTED]" {
		t.Errorf("expected password to be [REDACTED], got %v", result["password"])
	}
}

func TestHandle_SanitizeTrue_RedactsToken(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)
	logger := slog.New(handler)

	logger.Info("test", slog.String("api_token", "tk-12345"))

	result := parseLogOutput(t, &buf)
	if result["api_token"] != "[REDACTED]" {
		t.Errorf("expected api_token to be [REDACTED], got %v", result["api_token"])
	}
}

func TestHandle_SanitizeTrue_RedactsSecret(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)
	logger := slog.New(handler)

	logger.Info("test", slog.String("client_secret", "s3cr3t"))

	result := parseLogOutput(t, &buf)
	if result["client_secret"] != "[REDACTED]" {
		t.Errorf("expected client_secret to be [REDACTED], got %v", result["client_secret"])
	}
}

func TestHandle_SanitizeTrue_RedactsKey(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)
	logger := slog.New(handler)

	logger.Info("test", slog.String("api_key", "ak-xxxx"))

	result := parseLogOutput(t, &buf)
	if result["api_key"] != "[REDACTED]" {
		t.Errorf("expected api_key to be [REDACTED], got %v", result["api_key"])
	}
}

func TestHandle_SanitizeTrue_RedactsCredential(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)
	logger := slog.New(handler)

	logger.Info("test", slog.String("credential", "cred-value"))

	result := parseLogOutput(t, &buf)
	if result["credential"] != "[REDACTED]" {
		t.Errorf("expected credential to be [REDACTED], got %v", result["credential"])
	}
}

func TestHandle_SanitizeTrue_RedactsPassphrase(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)
	logger := slog.New(handler)

	logger.Info("test", slog.String("ssh_passphrase", "mypass"))

	result := parseLogOutput(t, &buf)
	if result["ssh_passphrase"] != "[REDACTED]" {
		t.Errorf("expected ssh_passphrase to be [REDACTED], got %v", result["ssh_passphrase"])
	}
}

func TestHandle_SanitizeTrue_RedactsAuth(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)
	logger := slog.New(handler)

	logger.Info("test", slog.String("auth_header", "Bearer xyz"))

	result := parseLogOutput(t, &buf)
	if result["auth_header"] != "[REDACTED]" {
		t.Errorf("expected auth_header to be [REDACTED], got %v", result["auth_header"])
	}
}

func TestHandle_SanitizeTrue_NonSensitivePassesThrough(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)
	logger := slog.New(handler)

	logger.Info("test",
		slog.String("username", "admin"),
		slog.String("host", "example.com"),
		slog.Int("port", 22),
	)

	result := parseLogOutput(t, &buf)
	if result["username"] != "admin" {
		t.Errorf("expected username to be 'admin', got %v", result["username"])
	}
	if result["host"] != "example.com" {
		t.Errorf("expected host to be 'example.com', got %v", result["host"])
	}
	// JSON numbers decode as float64
	if result["port"] != float64(22) {
		t.Errorf("expected port to be 22, got %v", result["port"])
	}
}

func TestHandle_SanitizeTrue_MixedSensitiveAndNonSensitive(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)
	logger := slog.New(handler)

	logger.Info("login attempt",
		slog.String("username", "admin"),
		slog.String("password", "secret123"),
		slog.String("host", "prod.example.com"),
	)

	result := parseLogOutput(t, &buf)
	if result["username"] != "admin" {
		t.Errorf("expected username to pass through, got %v", result["username"])
	}
	if result["password"] != "[REDACTED]" {
		t.Errorf("expected password to be redacted, got %v", result["password"])
	}
	if result["host"] != "prod.example.com" {
		t.Errorf("expected host to pass through, got %v", result["host"])
	}
}

func TestHandle_SanitizeFalse_NothingRedacted(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, false)
	logger := slog.New(handler)

	logger.Info("test",
		slog.String("password", "plaintext"),
		slog.String("token", "tk-visible"),
		slog.String("secret", "s3cr3t"),
	)

	result := parseLogOutput(t, &buf)
	if result["password"] != "plaintext" {
		t.Errorf("expected password to pass through when sanitize=false, got %v", result["password"])
	}
	if result["token"] != "tk-visible" {
		t.Errorf("expected token to pass through when sanitize=false, got %v", result["token"])
	}
	if result["secret"] != "s3cr3t" {
		t.Errorf("expected secret to pass through when sanitize=false, got %v", result["secret"])
	}
}

func TestHandle_SanitizeTrue_CaseInsensitiveKey(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)
	logger := slog.New(handler)

	logger.Info("test", slog.String("Password", "secret"))

	result := parseLogOutput(t, &buf)
	// The key is preserved as-is in the output, but sanitizeAttr lowercases for matching
	if result["Password"] != "[REDACTED]" {
		t.Errorf("expected Password (mixed case) to be redacted, got %v", result["Password"])
	}
}

func TestHandle_SanitizeTrue_SubstringMatch(t *testing.T) {
	// "key" is a sensitive key, so "my_key_value" should be redacted
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)
	logger := slog.New(handler)

	logger.Info("test", slog.String("my_key_value", "some-api-key"))

	result := parseLogOutput(t, &buf)
	if result["my_key_value"] != "[REDACTED]" {
		t.Errorf("expected my_key_value to be redacted (contains 'key'), got %v", result["my_key_value"])
	}
}

// ============================================================
// SanitizingHandler.WithAttrs tests
// ============================================================

func TestWithAttrs_SanitizeTrue_RedactsSensitive(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)

	withAttrs := handler.WithAttrs([]slog.Attr{
		slog.String("password", "secret123"),
		slog.String("username", "admin"),
	})

	logger := slog.New(withAttrs)
	logger.Info("test")

	result := parseLogOutput(t, &buf)
	if result["password"] != "[REDACTED]" {
		t.Errorf("expected password to be redacted via WithAttrs, got %v", result["password"])
	}
	if result["username"] != "admin" {
		t.Errorf("expected username to pass through via WithAttrs, got %v", result["username"])
	}
}

func TestWithAttrs_SanitizeFalse_PassesThrough(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, false)

	withAttrs := handler.WithAttrs([]slog.Attr{
		slog.String("password", "secret123"),
		slog.String("token", "tk-abc"),
	})

	logger := slog.New(withAttrs)
	logger.Info("test")

	result := parseLogOutput(t, &buf)
	if result["password"] != "secret123" {
		t.Errorf("expected password to pass through when sanitize=false, got %v", result["password"])
	}
	if result["token"] != "tk-abc" {
		t.Errorf("expected token to pass through when sanitize=false, got %v", result["token"])
	}
}

func TestWithAttrs_ReturnsNewSanitizingHandler(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)

	result := handler.WithAttrs([]slog.Attr{slog.String("foo", "bar")})

	sh, ok := result.(*SanitizingHandler)
	if !ok {
		t.Fatal("expected WithAttrs to return *SanitizingHandler")
	}
	if sh.sanitize != true {
		t.Error("expected sanitize to be preserved")
	}
}

// ============================================================
// SanitizingHandler.WithGroup tests
// ============================================================

func TestWithGroup_ReturnsNewSanitizingHandler(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)

	result := handler.WithGroup("mygroup")

	sh, ok := result.(*SanitizingHandler)
	if !ok {
		t.Fatal("expected WithGroup to return *SanitizingHandler")
	}
	if sh.sanitize != true {
		t.Error("expected sanitize to be preserved")
	}
}

func TestWithGroup_OutputContainsGroup(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)

	grouped := handler.WithGroup("mygroup")
	logger := slog.New(grouped)
	logger.Info("test", slog.String("field", "value"))

	result := parseLogOutput(t, &buf)
	groupData, ok := result["mygroup"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'mygroup' group in output, got %v", result)
	}
	if groupData["field"] != "value" {
		t.Errorf("expected field='value' in group, got %v", groupData["field"])
	}
}

// ============================================================
// Group attribute sanitization (nested groups with sensitive keys)
// ============================================================

func TestHandle_SanitizeTrue_NestedGroupAttrs(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)
	logger := slog.New(handler)

	logger.Info("test",
		slog.Group("connection",
			slog.String("host", "example.com"),
			slog.String("password", "secret"),
			slog.String("token", "tk-xxx"),
		),
	)

	result := parseLogOutput(t, &buf)
	conn, ok := result["connection"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'connection' group in output, got %v", result)
	}
	if conn["host"] != "example.com" {
		t.Errorf("expected host to pass through in group, got %v", conn["host"])
	}
	if conn["password"] != "[REDACTED]" {
		t.Errorf("expected password to be redacted in group, got %v", conn["password"])
	}
	if conn["token"] != "[REDACTED]" {
		t.Errorf("expected token to be redacted in group, got %v", conn["token"])
	}
}

func TestHandle_SanitizeTrue_DeeplyNestedGroup(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)
	logger := slog.New(handler)

	logger.Info("test",
		slog.Group("outer",
			slog.Group("inner",
				slog.String("secret", "deep-secret"),
				slog.String("name", "visible"),
			),
		),
	)

	result := parseLogOutput(t, &buf)
	outer, ok := result["outer"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'outer' group, got %v", result)
	}
	inner2, ok := outer["inner"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'inner' group, got %v", outer)
	}
	if inner2["secret"] != "[REDACTED]" {
		t.Errorf("expected deeply nested secret to be redacted, got %v", inner2["secret"])
	}
	if inner2["name"] != "visible" {
		t.Errorf("expected name to pass through in nested group, got %v", inner2["name"])
	}
}

func TestWithGroup_SanitizesAttrsInGroup(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)

	grouped := handler.WithGroup("ssh")
	logger := slog.New(grouped)
	logger.Info("connecting",
		slog.String("host", "prod.example.com"),
		slog.String("password", "s3cr3t"),
	)

	result := parseLogOutput(t, &buf)
	sshGroup, ok := result["ssh"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'ssh' group, got %v", result)
	}
	if sshGroup["host"] != "prod.example.com" {
		t.Errorf("expected host to pass through in WithGroup, got %v", sshGroup["host"])
	}
	if sshGroup["password"] != "[REDACTED]" {
		t.Errorf("expected password to be redacted in WithGroup, got %v", sshGroup["password"])
	}
}

// ============================================================
// Setup tests
// ============================================================

func TestSetup_DebugLevel(t *testing.T) {
	Setup("debug", true)
	handler := slog.Default().Handler()
	if !handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected debug level to be enabled after Setup('debug', ...)")
	}
}

func TestSetup_InfoLevel(t *testing.T) {
	Setup("info", true)
	handler := slog.Default().Handler()
	if !handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("expected info level to be enabled after Setup('info', ...)")
	}
	if handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected debug level to be disabled after Setup('info', ...)")
	}
}

func TestSetup_WarnLevel(t *testing.T) {
	Setup("warn", true)
	handler := slog.Default().Handler()
	if !handler.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("expected warn level to be enabled after Setup('warn', ...)")
	}
	if handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("expected info level to be disabled after Setup('warn', ...)")
	}
}

func TestSetup_ErrorLevel(t *testing.T) {
	Setup("error", true)
	handler := slog.Default().Handler()
	if !handler.Enabled(context.Background(), slog.LevelError) {
		t.Error("expected error level to be enabled after Setup('error', ...)")
	}
	if handler.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("expected warn level to be disabled after Setup('error', ...)")
	}
}

func TestSetup_UnknownLevel_DefaultsToInfo(t *testing.T) {
	Setup("unknown", true)
	handler := slog.Default().Handler()
	if !handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("expected info level to be enabled for unknown level string")
	}
	if handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected debug level to be disabled for unknown level string (defaults to info)")
	}
}

func TestSetup_EmptyLevel_DefaultsToInfo(t *testing.T) {
	Setup("", true)
	handler := slog.Default().Handler()
	if !handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("expected info level to be enabled for empty level string")
	}
	if handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected debug level to be disabled for empty level string")
	}
}

// ============================================================
// All sensitive keys table-driven test
// ============================================================

func TestHandle_SanitizeTrue_AllSensitiveKeys(t *testing.T) {
	keys := []string{
		"password",
		"secret",
		"token",
		"key",
		"credential",
		"passphrase",
		"auth",
	}

	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			var buf bytes.Buffer
			inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
			handler := NewSanitizingHandler(inner, true)
			logger := slog.New(handler)

			logger.Info("test", slog.String(key, "sensitive-value"))

			result := parseLogOutput(t, &buf)
			if result[key] != "[REDACTED]" {
				t.Errorf("expected key %q to be [REDACTED], got %v", key, result[key])
			}
		})
	}
}

// ============================================================
// Handle preserves message and level
// ============================================================

func TestHandle_PreservesMessageAndLevel(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)
	logger := slog.New(handler)

	logger.Warn("something happened", slog.String("detail", "info"))

	result := parseLogOutput(t, &buf)
	if result["msg"] != "something happened" {
		t.Errorf("expected msg 'something happened', got %v", result["msg"])
	}
	if result["level"] != "WARN" {
		t.Errorf("expected level WARN, got %v", result["level"])
	}
	if result["detail"] != "info" {
		t.Errorf("expected detail 'info', got %v", result["detail"])
	}
}

// ============================================================
// Handle with no attributes
// ============================================================

func TestHandle_SanitizeTrue_NoAttributes(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizingHandler(inner, true)
	logger := slog.New(handler)

	logger.Info("no attrs")

	result := parseLogOutput(t, &buf)
	if result["msg"] != "no attrs" {
		t.Errorf("expected msg 'no attrs', got %v", result["msg"])
	}
}
