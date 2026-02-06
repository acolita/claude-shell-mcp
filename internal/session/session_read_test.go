package session

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakepty"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakerand"
)

// --- findMarkerOnOwnLine tests ---

func TestFindMarkerOnOwnLine_AtStart(t *testing.T) {
	output := "___CMD_START_abc123___\nsome output"
	marker := "___CMD_START_abc123___"
	got := findMarkerOnOwnLine(output, marker)
	if got != 0 {
		t.Errorf("findMarkerOnOwnLine() = %d, want 0", got)
	}
}

func TestFindMarkerOnOwnLine_AfterNewline(t *testing.T) {
	output := "some async output\n___CMD_START_abc123___\ncommand output"
	marker := "___CMD_START_abc123___"
	got := findMarkerOnOwnLine(output, marker)
	want := len("some async output\n")
	if got != want {
		t.Errorf("findMarkerOnOwnLine() = %d, want %d", got, want)
	}
}

func TestFindMarkerOnOwnLine_NotFound(t *testing.T) {
	output := "some output with no markers at all"
	marker := "___CMD_START_abc123___"
	got := findMarkerOnOwnLine(output, marker)
	if got != -1 {
		t.Errorf("findMarkerOnOwnLine() = %d, want -1", got)
	}
}

func TestFindMarkerOnOwnLine_InMiddleOfLine(t *testing.T) {
	// Marker embedded in a line (e.g., the echo command) should not match
	// because findMarkerOnOwnLine requires it after a newline
	output := "echo '___CMD_START_abc123___'; rest"
	marker := "___CMD_START_abc123___"
	got := findMarkerOnOwnLine(output, marker)
	// This will actually match at start since the string starts with "echo" not the marker
	// The marker is embedded after "echo '" so it should NOT be found at start of a line
	if got != -1 {
		// It should be -1 because the marker does not start a line
		// The marker appears at position 6, not after a newline
		t.Errorf("findMarkerOnOwnLine() = %d, want -1 (marker not on its own line)", got)
	}
}

func TestFindMarkerOnOwnLine_EmptyOutput(t *testing.T) {
	got := findMarkerOnOwnLine("", "___CMD_START_abc___")
	if got != -1 {
		t.Errorf("findMarkerOnOwnLine() = %d, want -1 for empty output", got)
	}
}

func TestFindMarkerOnOwnLine_MultipleOccurrences(t *testing.T) {
	// Should return the first occurrence on its own line
	output := "prefix\n___CMD_START_abc___\nmiddle\n___CMD_START_abc___\nend"
	marker := "___CMD_START_abc___"
	got := findMarkerOnOwnLine(output, marker)
	want := len("prefix\n")
	if got != want {
		t.Errorf("findMarkerOnOwnLine() = %d, want %d (first occurrence)", got, want)
	}
}

// --- parseMarkedOutput tests ---

func TestParseMarkedOutput_BasicCase(t *testing.T) {
	sess := &Session{}
	startMarker := "___CMD_START_abc___"
	endMarker := "___CMD_END_abc___"

	output := startMarker + "\nhello world\n" + endMarker + "0\n"
	asyncOutput, cmdOutput := sess.parseMarkedOutput(output, startMarker, endMarker, "echo hello")

	if asyncOutput != "" {
		t.Errorf("asyncOutput = %q, want empty", asyncOutput)
	}
	if cmdOutput != "hello world" {
		t.Errorf("cmdOutput = %q, want %q", cmdOutput, "hello world")
	}
}

func TestParseMarkedOutput_WithAsyncOutput(t *testing.T) {
	sess := &Session{}
	startMarker := "___CMD_START_abc___"
	endMarker := "___CMD_END_abc___"

	output := "background job done\n" + startMarker + "\ncmd output\n" + endMarker + "0\n"
	asyncOutput, cmdOutput := sess.parseMarkedOutput(output, startMarker, endMarker, "cmd")

	if asyncOutput != "background job done" {
		t.Errorf("asyncOutput = %q, want %q", asyncOutput, "background job done")
	}
	if cmdOutput != "cmd output" {
		t.Errorf("cmdOutput = %q, want %q", cmdOutput, "cmd output")
	}
}

func TestParseMarkedOutput_NoStartMarker(t *testing.T) {
	sess := &Session{}
	startMarker := "___CMD_START_abc___"
	endMarker := "___CMD_END_abc___"

	output := "some random output"
	asyncOutput, cmdOutput := sess.parseMarkedOutput(output, startMarker, endMarker, "cmd")

	if asyncOutput != "some random output" {
		t.Errorf("asyncOutput = %q, want %q", asyncOutput, "some random output")
	}
	if cmdOutput != "" {
		t.Errorf("cmdOutput = %q, want empty", cmdOutput)
	}
}

func TestParseMarkedOutput_NoEndMarker(t *testing.T) {
	sess := &Session{}
	startMarker := "___CMD_START_abc___"
	endMarker := "___CMD_END_abc___"

	output := startMarker + "\npartial output still running"
	asyncOutput, cmdOutput := sess.parseMarkedOutput(output, startMarker, endMarker, "cmd")

	if asyncOutput != "" {
		t.Errorf("asyncOutput = %q, want empty", asyncOutput)
	}
	if cmdOutput != "partial output still running" {
		t.Errorf("cmdOutput = %q, want %q", cmdOutput, "partial output still running")
	}
}

func TestParseMarkedOutput_CRLFNormalization(t *testing.T) {
	sess := &Session{}
	startMarker := "___CMD_START_abc___"
	endMarker := "___CMD_END_abc___"

	output := startMarker + "\r\nhello\r\nworld\r\n" + endMarker + "0\r\n"
	_, cmdOutput := sess.parseMarkedOutput(output, startMarker, endMarker, "cmd")

	if strings.Contains(cmdOutput, "\r") {
		t.Errorf("cmdOutput still contains \\r: %q", cmdOutput)
	}
	if cmdOutput != "hello\nworld" {
		t.Errorf("cmdOutput = %q, want %q", cmdOutput, "hello\nworld")
	}
}

func TestParseMarkedOutput_AsyncShellPromptFiltered(t *testing.T) {
	sess := &Session{}
	startMarker := "___CMD_START_abc___"
	endMarker := "___CMD_END_abc___"

	output := "$ echo hello\n$ \n" + startMarker + "\noutput\n" + endMarker + "0\n"
	asyncOutput, _ := sess.parseMarkedOutput(output, startMarker, endMarker, "echo hello")

	// Shell prompt lines (starting with "$ ") should be filtered
	if strings.Contains(asyncOutput, "$ ") {
		t.Errorf("asyncOutput should not contain shell prompts: %q", asyncOutput)
	}
}

func TestParseMarkedOutput_MultilineOutput(t *testing.T) {
	sess := &Session{}
	startMarker := "___CMD_START_abc___"
	endMarker := "___CMD_END_abc___"

	output := startMarker + "\nline1\nline2\nline3\n" + endMarker + "0\n"
	_, cmdOutput := sess.parseMarkedOutput(output, startMarker, endMarker, "cmd")

	if cmdOutput != "line1\nline2\nline3" {
		t.Errorf("cmdOutput = %q, want %q", cmdOutput, "line1\nline2\nline3")
	}
}

// --- cleanAsyncOutput tests ---

func TestCleanAsyncOutput_RemovesShellPrompts(t *testing.T) {
	sess := &Session{}
	output := "$ ls\nfile1.txt\n$ cd /tmp\n"
	got := sess.cleanAsyncOutput(output)
	if strings.Contains(got, "$ ") {
		t.Errorf("cleanAsyncOutput should remove lines starting with '$ ': %q", got)
	}
	if got != "file1.txt" {
		t.Errorf("cleanAsyncOutput = %q, want %q", got, "file1.txt")
	}
}

func TestCleanAsyncOutput_RemovesEmptyLines(t *testing.T) {
	sess := &Session{}
	output := "\n\nactual output\n\n"
	got := sess.cleanAsyncOutput(output)
	if got != "actual output" {
		t.Errorf("cleanAsyncOutput = %q, want %q", got, "actual output")
	}
}

func TestCleanAsyncOutput_EmptyInput(t *testing.T) {
	sess := &Session{}
	got := sess.cleanAsyncOutput("")
	if got != "" {
		t.Errorf("cleanAsyncOutput(\"\") = %q, want empty", got)
	}
}

func TestCleanAsyncOutput_OnlyPrompts(t *testing.T) {
	sess := &Session{}
	output := "$ \n$ ls\n"
	got := sess.cleanAsyncOutput(output)
	if got != "" {
		t.Errorf("cleanAsyncOutput = %q, want empty (all prompt lines)", got)
	}
}

// --- cleanCommandOutput tests ---

func TestCleanCommandOutput_RemovesEndMarkerLines(t *testing.T) {
	sess := &Session{}
	output := "hello world\n___CMD_END_abc___0"
	got := sess.cleanCommandOutput(output, "echo hello", "___CMD_START_abc___", "___CMD_END_abc___")
	if strings.Contains(got, "___CMD_END_") {
		t.Errorf("cleanCommandOutput should remove end marker lines: %q", got)
	}
	if got != "hello world" {
		t.Errorf("cleanCommandOutput = %q, want %q", got, "hello world")
	}
}

func TestCleanCommandOutput_EmptyOutput(t *testing.T) {
	sess := &Session{}
	got := sess.cleanCommandOutput("", "cmd", "start", "end")
	if got != "" {
		t.Errorf("cleanCommandOutput(\"\") = %q, want empty", got)
	}
}

func TestCleanCommandOutput_OnlyEndMarker(t *testing.T) {
	sess := &Session{}
	output := "___CMD_END_abc___0"
	got := sess.cleanCommandOutput(output, "cmd", "___CMD_START_abc___", "___CMD_END_abc___")
	if got != "" {
		t.Errorf("cleanCommandOutput = %q, want empty when only end marker", got)
	}
}

// --- extractExitCodeWithMarker tests ---

func TestExtractExitCodeWithMarker_Zero(t *testing.T) {
	sess := &Session{}
	endMarker := "___CMD_END_abc___"
	output := "some output\n" + endMarker + "0\n"
	code, found := sess.extractExitCodeWithMarker(output, endMarker)
	if !found {
		t.Fatal("expected to find exit code")
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestExtractExitCodeWithMarker_NonZero(t *testing.T) {
	sess := &Session{}
	endMarker := "___CMD_END_abc___"
	output := "error output\n" + endMarker + "127\n"
	code, found := sess.extractExitCodeWithMarker(output, endMarker)
	if !found {
		t.Fatal("expected to find exit code")
	}
	if code != 127 {
		t.Errorf("exit code = %d, want 127", code)
	}
}

func TestExtractExitCodeWithMarker_NotFound(t *testing.T) {
	sess := &Session{}
	endMarker := "___CMD_END_abc___"
	output := "some output without marker"
	_, found := sess.extractExitCodeWithMarker(output, endMarker)
	if found {
		t.Error("expected not to find exit code")
	}
}

func TestExtractExitCodeWithMarker_CRLFNewlines(t *testing.T) {
	sess := &Session{}
	endMarker := "___CMD_END_abc___"
	output := "some output\r\n" + endMarker + "2\r\n"
	code, found := sess.extractExitCodeWithMarker(output, endMarker)
	if !found {
		t.Fatal("expected to find exit code with CRLF")
	}
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestExtractExitCodeWithMarker_InMiddleOfLine(t *testing.T) {
	// curl output without trailing newline: "000___CMD_END_abc___7"
	sess := &Session{}
	endMarker := "___CMD_END_abc___"
	output := "000" + endMarker + "7\n"
	code, found := sess.extractExitCodeWithMarker(output, endMarker)
	if !found {
		t.Fatal("expected to find exit code in middle of line")
	}
	if code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
}

// --- extractExitCode (legacy) tests ---

func TestExtractExitCode_LegacyMarker(t *testing.T) {
	sess := &Session{}
	output := "output\n" + endMarker + "0\n"
	code, found := sess.extractExitCode(output)
	if !found {
		t.Fatal("expected to find legacy exit code")
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestExtractExitCode_DynamicMarker(t *testing.T) {
	sess := &Session{}
	output := "output\n___CMD_END_deadbeef___42\n"
	code, found := sess.extractExitCode(output)
	if !found {
		t.Fatal("expected to find dynamic exit code")
	}
	if code != 42 {
		t.Errorf("exit code = %d, want 42", code)
	}
}

func TestExtractExitCode_NotFound(t *testing.T) {
	sess := &Session{}
	output := "some output\nno markers here"
	_, found := sess.extractExitCode(output)
	if found {
		t.Error("expected not to find exit code")
	}
}

func TestExtractExitCode_CRLFHandling(t *testing.T) {
	sess := &Session{}
	output := "output\r\n___CMD_END_aabb___1\r\n"
	code, found := sess.extractExitCode(output)
	if !found {
		t.Fatal("expected to find exit code with CRLF")
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

// --- cleanOutput tests ---

func TestCleanOutput_RemovesCommandEcho(t *testing.T) {
	sess := &Session{}
	output := "echo hello world\nhello world\n___CMD_END_MARKER___0"
	got := sess.cleanOutput(output, "echo hello world")
	if strings.Contains(got, "echo hello world") {
		t.Errorf("cleanOutput should remove command echo: %q", got)
	}
	if got != "hello world" {
		t.Errorf("cleanOutput = %q, want %q", got, "hello world")
	}
}

func TestCleanOutput_RemovesShellPromptLines(t *testing.T) {
	sess := &Session{}
	output := "$ echo hello\nhello\n$ \n___CMD_END_MARKER___0"
	got := sess.cleanOutput(output, "echo hello")
	if strings.Contains(got, "$ ") {
		t.Errorf("cleanOutput should remove prompt lines: %q", got)
	}
}

func TestCleanOutput_RemovesDynamicMarkers(t *testing.T) {
	sess := &Session{}
	output := "___CMD_START_abc123___\nhello\n___CMD_END_abc123___0"
	got := sess.cleanOutput(output, "echo hello")
	if strings.Contains(got, "___CMD_START_") || strings.Contains(got, "___CMD_END_") {
		t.Errorf("cleanOutput should remove markers: %q", got)
	}
}

func TestCleanOutput_TrimsLeadingTrailingEmpty(t *testing.T) {
	sess := &Session{}
	output := "\n\nhello\n\n"
	got := sess.cleanOutput(output, "")
	if got != "hello" {
		t.Errorf("cleanOutput = %q, want %q", got, "hello")
	}
}

func TestCleanOutput_EmptyOutput(t *testing.T) {
	sess := &Session{}
	got := sess.cleanOutput("", "cmd")
	if got != "" {
		t.Errorf("cleanOutput(\"\") = %q, want empty", got)
	}
}

func TestCleanOutput_CRNormalization(t *testing.T) {
	sess := &Session{}
	output := "hello\r\nworld\r\n"
	got := sess.cleanOutput(output, "")
	if strings.Contains(got, "\r") {
		t.Errorf("cleanOutput should remove \\r: %q", got)
	}
}

// --- containsPeakTTYSignal tests ---

func TestContainsPeakTTYSignal_Present(t *testing.T) {
	// 13 NUL bytes
	output := "some output" + peakTTYSignal + "more output"
	if !containsPeakTTYSignal(output) {
		t.Error("expected to detect peak-tty signal (13 NUL bytes)")
	}
}

func TestContainsPeakTTYSignal_NotPresent(t *testing.T) {
	output := "normal output without NUL bytes"
	if containsPeakTTYSignal(output) {
		t.Error("expected no peak-tty signal")
	}
}

func TestContainsPeakTTYSignal_TooFewNULs(t *testing.T) {
	// Only 12 NUL bytes
	output := "output" + "\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00" + "rest"
	if containsPeakTTYSignal(output) {
		t.Error("12 NUL bytes should not trigger peak-tty signal (need 13)")
	}
}

func TestContainsPeakTTYSignal_Empty(t *testing.T) {
	if containsPeakTTYSignal("") {
		t.Error("empty string should not contain peak-tty signal")
	}
}

// --- stripANSI additional tests ---

func TestStripANSI_OSCSequences(t *testing.T) {
	// OSC (Operating System Command) sequences like terminal title
	input := "\x1b]0;title\x07some text"
	got := stripANSI(input)
	if got != "some text" {
		t.Errorf("stripANSI(%q) = %q, want %q", input, got, "some text")
	}
}

func TestStripANSI_EmptyInput(t *testing.T) {
	got := stripANSI("")
	if got != "" {
		t.Errorf("stripANSI(\"\") = %q, want empty", got)
	}
}

func TestStripANSI_NoEscapes(t *testing.T) {
	input := "just plain text"
	got := stripANSI(input)
	if got != input {
		t.Errorf("stripANSI(%q) = %q, want %q", input, got, input)
	}
}

func TestStripANSI_MultipleSequences(t *testing.T) {
	input := "\x1b[31m\x1b[1mred bold\x1b[0m \x1b[32mgreen\x1b[0m"
	got := stripANSI(input)
	if got != "red bold green" {
		t.Errorf("stripANSI = %q, want %q", got, "red bold green")
	}
}

// --- generateCommandID tests ---

func TestGenerateCommandID_Length(t *testing.T) {
	sess := &Session{
		random: fakerand.NewSequential(),
		clock:  fakeclock.New(time.Now()),
	}
	id := sess.generateCommandID()
	if len(id) != 8 {
		t.Errorf("command ID length = %d, want 8 (hex-encoded 4 bytes)", len(id))
	}
}

func TestGenerateCommandID_Deterministic(t *testing.T) {
	rand := fakerand.New([]byte{0xDE, 0xAD, 0xBE, 0xEF})
	sess := &Session{
		random: rand,
		clock:  fakeclock.New(time.Now()),
	}
	id := sess.generateCommandID()
	if id != "deadbeef" {
		t.Errorf("command ID = %q, want %q", id, "deadbeef")
	}
}

func TestGenerateCommandID_Sequential(t *testing.T) {
	rand := fakerand.NewSequential()
	sess := &Session{
		random: rand,
		clock:  fakeclock.New(time.Now()),
	}
	id1 := sess.generateCommandID()
	id2 := sess.generateCommandID()
	if id1 == id2 {
		t.Errorf("sequential IDs should differ: both %q", id1)
	}
}

// --- buildWrappedCommand tests ---

func TestBuildWrappedCommand_Format(t *testing.T) {
	sess := &Session{}
	cmd := sess.buildWrappedCommand("ls -la", "abc12345")
	if !strings.Contains(cmd, "___CMD_START_abc12345___") {
		t.Errorf("wrapped command should contain start marker: %q", cmd)
	}
	if !strings.Contains(cmd, "___CMD_END_abc12345___") {
		t.Errorf("wrapped command should contain end marker: %q", cmd)
	}
	if !strings.Contains(cmd, "ls -la") {
		t.Errorf("wrapped command should contain original command: %q", cmd)
	}
	if !strings.HasSuffix(cmd, "\n") {
		t.Errorf("wrapped command should end with newline: %q", cmd)
	}
}

func TestBuildWrappedCommand_EscapesSingleQuotes(t *testing.T) {
	sess := &Session{}
	cmd := sess.buildWrappedCommand("echo 'hello'", "abc12345")
	// Single quotes in the command should be escaped
	if !strings.Contains(cmd, `'\''`) {
		t.Errorf("wrapped command should escape single quotes: %q", cmd)
	}
}

func TestBuildWrappedCommand_IncludesExitCodeCapture(t *testing.T) {
	sess := &Session{}
	cmd := sess.buildWrappedCommand("ls", "abc12345")
	// Should contain $? for exit code capture
	if !strings.Contains(cmd, "$?") {
		t.Errorf("wrapped command should capture exit code with $?: %q", cmd)
	}
}

// --- getTimeout tests ---

func TestGetTimeout_CustomValue(t *testing.T) {
	sess := &Session{}
	timeout := sess.getTimeout(5000)
	if timeout != 5*time.Second {
		t.Errorf("getTimeout(5000) = %v, want 5s", timeout)
	}
}

func TestGetTimeout_ZeroDefaultsTo30s(t *testing.T) {
	sess := &Session{}
	timeout := sess.getTimeout(0)
	if timeout != 30*time.Second {
		t.Errorf("getTimeout(0) = %v, want 30s", timeout)
	}
}

func TestGetTimeout_SmallValue(t *testing.T) {
	sess := &Session{}
	timeout := sess.getTimeout(100)
	if timeout != 100*time.Millisecond {
		t.Errorf("getTimeout(100) = %v, want 100ms", timeout)
	}
}

// --- applyMultilineDelay tests ---

func TestApplyMultilineDelay_SingleLine(t *testing.T) {
	clock := fakeclock.New(time.Now())
	sess := &Session{clock: clock}
	// Single line command should not cause any sleep
	sess.applyMultilineDelay("ls -la")
	// No panic/error means success - fakeclock.Sleep is a no-op
}

func TestApplyMultilineDelay_MultiLine(t *testing.T) {
	clock := fakeclock.New(time.Now())
	sess := &Session{clock: clock}
	// Multi-line command should apply delay
	sess.applyMultilineDelay("line1\nline2\nline3")
	// No panic/error means success
}

// --- parseEnvOutput tests ---

func TestParseEnvOutput_BasicKeyValue(t *testing.T) {
	output := "HOME=/home/user\nPATH=/usr/bin:/bin\nSHELL=/bin/bash"
	result := parseEnvOutput(output)

	if result["HOME"] != "/home/user" {
		t.Errorf("HOME = %q, want %q", result["HOME"], "/home/user")
	}
	if result["PATH"] != "/usr/bin:/bin" {
		t.Errorf("PATH = %q, want %q", result["PATH"], "/usr/bin:/bin")
	}
	if result["SHELL"] != "/bin/bash" {
		t.Errorf("SHELL = %q, want %q", result["SHELL"], "/bin/bash")
	}
}

func TestParseEnvOutput_SkipsInternalVars(t *testing.T) {
	output := "HOME=/home/user\n_=/usr/bin/env\nSHLVL=1\nOLDPWD=/tmp"
	result := parseEnvOutput(output)

	if _, ok := result["_"]; ok {
		t.Error("should skip vars starting with _")
	}
	if _, ok := result["SHLVL"]; ok {
		t.Error("should skip SHLVL")
	}
	if _, ok := result["OLDPWD"]; ok {
		t.Error("should skip OLDPWD")
	}
}

func TestParseEnvOutput_SkipsPromptAndCommand(t *testing.T) {
	output := "$ env\nHOME=/home/user\n$ "
	result := parseEnvOutput(output)

	if _, ok := result["$ env"]; ok {
		t.Error("should not parse prompt lines")
	}
	if result["HOME"] != "/home/user" {
		t.Errorf("HOME = %q, want %q", result["HOME"], "/home/user")
	}
}

func TestParseEnvOutput_EmptyOutput(t *testing.T) {
	result := parseEnvOutput("")
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}
}

func TestParseEnvOutput_ValueWithEquals(t *testing.T) {
	// Values can contain = signs
	output := "LS_COLORS=*.tar=01;31:*.gz=01;31"
	result := parseEnvOutput(output)
	if result["LS_COLORS"] != "*.tar=01;31:*.gz=01;31" {
		t.Errorf("LS_COLORS = %q, want %q", result["LS_COLORS"], "*.tar=01;31:*.gz=01;31")
	}
}

// --- parseAliasOutput tests ---

func TestParseAliasOutput_BashFormat(t *testing.T) {
	output := "alias ll='ls -la'\nalias gs='git status'"
	result := parseAliasOutput(output)

	if result["ll"] != "ls -la" {
		t.Errorf("ll = %q, want %q", result["ll"], "ls -la")
	}
	if result["gs"] != "git status" {
		t.Errorf("gs = %q, want %q", result["gs"], "git status")
	}
}

func TestParseAliasOutput_ZshFormat(t *testing.T) {
	output := "ll='ls -la'\ngs='git status'"
	result := parseAliasOutput(output)

	if result["ll"] != "ls -la" {
		t.Errorf("ll = %q, want %q", result["ll"], "ls -la")
	}
}

func TestParseAliasOutput_SkipsPromptAndCommand(t *testing.T) {
	output := "$ alias\nalias ll='ls -la'\n$ "
	result := parseAliasOutput(output)

	if len(result) != 1 {
		t.Errorf("expected 1 alias, got %d", len(result))
	}
	if result["ll"] != "ls -la" {
		t.Errorf("ll = %q, want %q", result["ll"], "ls -la")
	}
}

func TestParseAliasOutput_EmptyOutput(t *testing.T) {
	result := parseAliasOutput("")
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}
}

func TestParseAliasOutput_DoubleQuotes(t *testing.T) {
	output := `alias greet="echo hello"`
	result := parseAliasOutput(output)
	if result["greet"] != "echo hello" {
		t.Errorf("greet = %q, want %q", result["greet"], "echo hello")
	}
}

// --- GetShellInfo tests ---

func TestGetShellInfo_Bash(t *testing.T) {
	sess := &Session{Shell: "/bin/bash"}
	info := sess.GetShellInfo()
	if info.Type != "bash" {
		t.Errorf("Type = %q, want %q", info.Type, "bash")
	}
	if !info.SupportsHistory {
		t.Error("bash should support history")
	}
}

func TestGetShellInfo_Zsh(t *testing.T) {
	sess := &Session{Shell: "/usr/bin/zsh"}
	info := sess.GetShellInfo()
	if info.Type != "zsh" {
		t.Errorf("Type = %q, want %q", info.Type, "zsh")
	}
	if !info.SupportsHistory {
		t.Error("zsh should support history")
	}
}

func TestGetShellInfo_Sh(t *testing.T) {
	sess := &Session{Shell: "/bin/sh"}
	info := sess.GetShellInfo()
	if info.Type != "sh" {
		t.Errorf("Type = %q, want %q", info.Type, "sh")
	}
	if info.SupportsHistory {
		t.Error("sh should not support history")
	}
}

func TestGetShellInfo_Dash(t *testing.T) {
	sess := &Session{Shell: "/bin/dash"}
	info := sess.GetShellInfo()
	if info.Type != "sh" {
		t.Errorf("Type = %q, want %q", info.Type, "sh")
	}
}

func TestGetShellInfo_Unknown(t *testing.T) {
	sess := &Session{Shell: "/usr/bin/fish"}
	info := sess.GetShellInfo()
	if info.Type != "unknown" {
		t.Errorf("Type = %q, want %q", info.Type, "unknown")
	}
}

func TestGetShellInfo_NoPath(t *testing.T) {
	sess := &Session{Shell: "bash"}
	info := sess.GetShellInfo()
	if info.Type != "bash" {
		t.Errorf("Type = %q, want %q", info.Type, "bash")
	}
}

// --- checkForCompletion tests ---

func TestCheckForCompletion_Found(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Now())
	sess := NewSession("test_check_completion", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(config.DefaultConfig()),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	cmdID := "11223344"
	startM := startMarkerPrefix + cmdID + markerSuffix
	endM := endMarkerPrefix + cmdID + markerSuffix
	ctx := newExecContext(cmdID, startM, endM, "ls")

	sess.State = StateRunning
	sess.outputBuffer.WriteString(startM + "\nfile.txt\n" + endM + "0\n")

	// Queue a response for updateCwd's "pwd\n" command
	pty.AddResponse("/home/user\n")

	result, found := sess.checkForCompletion(ctx)
	if !found {
		t.Fatal("expected completion to be found")
	}
	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}
	if sess.State != StateIdle {
		t.Errorf("State = %v, want %v after completion", sess.State, StateIdle)
	}
}

func TestCheckForCompletion_NotFound(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Now())
	sess := NewSession("test_check_no_completion", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(config.DefaultConfig()),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	cmdID := "55667788"
	startM := startMarkerPrefix + cmdID + markerSuffix
	endM := endMarkerPrefix + cmdID + markerSuffix
	ctx := newExecContext(cmdID, startM, endM, "sleep 10")

	sess.outputBuffer.WriteString(startM + "\npartial...\n")

	_, found := sess.checkForCompletion(ctx)
	if found {
		t.Error("expected completion NOT to be found for partial output")
	}
}

// --- checkForPeakTTYSignal tests ---

func TestCheckForPeakTTYSignal_Found(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Now())
	sess := NewSession("test_peak_signal", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(config.DefaultConfig()),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	cmdID := "aabb0011"
	startM := startMarkerPrefix + cmdID + markerSuffix
	endM := endMarkerPrefix + cmdID + markerSuffix
	ctx := newExecContext(cmdID, startM, endM, "interactive")

	sess.State = StateRunning
	sess.outputBuffer.WriteString(startM + "\nwait" + peakTTYSignal)

	result, found := sess.checkForPeakTTYSignal(ctx)
	if !found {
		t.Fatal("expected peak-tty signal to be found")
	}
	if result.Status != "awaiting_input" {
		t.Errorf("Status = %q, want %q", result.Status, "awaiting_input")
	}
	if sess.State != StateAwaitingInput {
		t.Errorf("State = %v, want %v after peak-tty", sess.State, StateAwaitingInput)
	}
}

func TestCheckForPeakTTYSignal_NotFound(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Now())
	sess := NewSession("test_no_peak", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(config.DefaultConfig()),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	cmdID := "ccdd0022"
	startM := startMarkerPrefix + cmdID + markerSuffix
	endM := endMarkerPrefix + cmdID + markerSuffix
	ctx := newExecContext(cmdID, startM, endM, "ls")

	sess.outputBuffer.WriteString(startM + "\nnormal output\n")

	_, found := sess.checkForPeakTTYSignal(ctx)
	if found {
		t.Error("expected peak-tty signal NOT to be found")
	}
}

// --- checkForPasswordPrompt tests ---

func TestCheckForPasswordPrompt_Found(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Now())
	sess := NewSession("test_password_prompt", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(config.DefaultConfig()),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	cmdID := "eeff1122"
	startM := startMarkerPrefix + cmdID + markerSuffix
	endM := endMarkerPrefix + cmdID + markerSuffix
	ctx := newExecContext(cmdID, startM, endM, "sudo apt update")

	sess.State = StateRunning
	sess.outputBuffer.WriteString(startM + "\n[sudo] password for user: ")

	strippedOutput := stripANSI(sess.outputBuffer.String())
	result, found := sess.checkForPasswordPrompt(ctx, strippedOutput)
	if !found {
		t.Fatal("expected password prompt to be detected")
	}
	if result.Status != "awaiting_input" {
		t.Errorf("Status = %q, want %q", result.Status, "awaiting_input")
	}
	if result.PromptType != "password" {
		t.Errorf("PromptType = %q, want %q", result.PromptType, "password")
	}
	if !result.MaskInput {
		t.Error("MaskInput should be true for password prompts")
	}
}

func TestCheckForPasswordPrompt_NotFound(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Now())
	sess := NewSession("test_no_password", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(config.DefaultConfig()),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	cmdID := "aabb3344"
	startM := startMarkerPrefix + cmdID + markerSuffix
	endM := endMarkerPrefix + cmdID + markerSuffix
	ctx := newExecContext(cmdID, startM, endM, "ls")

	sess.outputBuffer.WriteString(startM + "\nnormal output\n")
	strippedOutput := stripANSI(sess.outputBuffer.String())

	_, found := sess.checkForPasswordPrompt(ctx, strippedOutput)
	if found {
		t.Error("expected password prompt NOT to be found")
	}
}

// --- Exec integration with output marker processing ---

func TestExec_MarkerBasedOutputIsolation(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	rand := fakerand.New([]byte{0xAA, 0xBB, 0xCC, 0xDD})
	cfg := config.DefaultConfig()

	sess := NewSession("test_markers", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithSessionRandom(rand),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	cmdID := "aabbccdd"
	pty.AddResponse(buildCommandOutput(cmdID, "marker output", 0))

	result, err := sess.Exec("test cmd", 5000)
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", result.ExitCode)
	}
	if result.CommandID != cmdID {
		t.Errorf("CommandID = %q, want %q", result.CommandID, cmdID)
	}
}

func TestExec_WithAsyncOutput(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	rand := fakerand.New([]byte{0x10, 0x20, 0x30, 0x40})
	cfg := config.DefaultConfig()

	sess := NewSession("test_async", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithSessionRandom(rand),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	cmdID := "10203040"
	startMarker := startMarkerPrefix + cmdID + markerSuffix
	endMarker := endMarkerPrefix + cmdID + markerSuffix

	// Simulate async output before command markers
	response := fmt.Sprintf("background job completed\n%s\ncommand result\n%s0\n", startMarker, endMarker)
	pty.AddResponse(response)

	result, err := sess.Exec("my_command", 5000)
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}
	if result.AsyncOutput == "" {
		t.Error("expected async output to be captured")
	}
	if !strings.Contains(result.AsyncOutput, "background job completed") {
		t.Errorf("AsyncOutput = %q, want containing %q", result.AsyncOutput, "background job completed")
	}
}

func TestExec_ExitCode42(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	rand := fakerand.New([]byte{0x55, 0x66, 0x77, 0x88})
	cfg := config.DefaultConfig()

	sess := NewSession("test_exit42", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithSessionRandom(rand),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	cmdID := "55667788"
	pty.AddResponse(buildCommandOutput(cmdID, "failed operation", 42))

	result, err := sess.Exec("failing_command", 5000)
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}
	if result.ExitCode == nil || *result.ExitCode != 42 {
		t.Errorf("ExitCode = %v, want 42", result.ExitCode)
	}
}

// --- execContext and builder tests ---

func TestNewExecContext(t *testing.T) {
	ctx := newExecContext("abc123", "___CMD_START_abc123___", "___CMD_END_abc123___", "ls -la")
	if ctx.commandID != "abc123" {
		t.Errorf("commandID = %q, want %q", ctx.commandID, "abc123")
	}
	if ctx.startMarker != "___CMD_START_abc123___" {
		t.Errorf("startMarker = %q, want %q", ctx.startMarker, "___CMD_START_abc123___")
	}
	if ctx.endMarker != "___CMD_END_abc123___" {
		t.Errorf("endMarker = %q, want %q", ctx.endMarker, "___CMD_END_abc123___")
	}
	if ctx.command != "ls -la" {
		t.Errorf("command = %q, want %q", ctx.command, "ls -la")
	}
}

func TestBuildCompletedResult(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Now())
	sess := NewSession("test_build", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(config.DefaultConfig()),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	cmdID := "aabb1122"
	startM := startMarkerPrefix + cmdID + markerSuffix
	endM := endMarkerPrefix + cmdID + markerSuffix
	ctx := newExecContext(cmdID, startM, endM, "test")

	// Populate output buffer
	sess.outputBuffer.WriteString(startM + "\nhello world\n" + endM + "0\n")

	result := sess.buildCompletedResult(ctx, 0, "/home/user")
	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", result.ExitCode)
	}
	if result.CommandID != cmdID {
		t.Errorf("CommandID = %q, want %q", result.CommandID, cmdID)
	}
	if result.Cwd != "/home/user" {
		t.Errorf("Cwd = %q, want %q", result.Cwd, "/home/user")
	}
}

func TestBuildTimeoutResult(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Now())
	sess := NewSession("test_timeout", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(config.DefaultConfig()),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	cmdID := "ccdd4455"
	startM := startMarkerPrefix + cmdID + markerSuffix
	endM := endMarkerPrefix + cmdID + markerSuffix
	ctx := newExecContext(cmdID, startM, endM, "sleep 100")

	sess.outputBuffer.WriteString(startM + "\npartial output\n")

	result := sess.buildTimeoutResult(ctx)
	if result.Status != "timeout" {
		t.Errorf("Status = %q, want %q", result.Status, "timeout")
	}
	if result.CommandID != cmdID {
		t.Errorf("CommandID = %q, want %q", result.CommandID, cmdID)
	}
	if result.ExitCode != nil {
		t.Errorf("ExitCode should be nil for timeout, got %v", *result.ExitCode)
	}
}

func TestBuildPeakTTYResult(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Now())
	sess := NewSession("test_peak", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(config.DefaultConfig()),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	cmdID := "eeff0011"
	startM := startMarkerPrefix + cmdID + markerSuffix
	endM := endMarkerPrefix + cmdID + markerSuffix
	ctx := newExecContext(cmdID, startM, endM, "interactive_cmd")

	output := startM + "\nwaiting for input" + peakTTYSignal
	result := sess.buildPeakTTYResult(ctx, output)

	if result.Status != "awaiting_input" {
		t.Errorf("Status = %q, want %q", result.Status, "awaiting_input")
	}
	if result.PromptType != "interactive" {
		t.Errorf("PromptType = %q, want %q", result.PromptType, "interactive")
	}
	if result.Hint != hintPeakTTYWaiting {
		t.Errorf("Hint = %q, want %q", result.Hint, hintPeakTTYWaiting)
	}
	// NUL bytes should be stripped from stdout
	if strings.Contains(result.Stdout, "\x00") {
		t.Error("Stdout should not contain NUL bytes")
	}
}

// --- validateExecPreconditions tests ---

func TestValidateExecPreconditions_Closed(t *testing.T) {
	sess := &Session{State: StateClosed, pty: fakepty.New()}
	err := sess.validateExecPreconditions()
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("expected 'closed' error, got %v", err)
	}
}

func TestValidateExecPreconditions_NilPTY(t *testing.T) {
	sess := &Session{State: StateIdle}
	err := sess.validateExecPreconditions()
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected 'not initialized' error, got %v", err)
	}
}

func TestValidateExecPreconditions_Valid(t *testing.T) {
	sess := &Session{State: StateIdle, pty: fakepty.New()}
	err := sess.validateExecPreconditions()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- validateAwaitingInputState tests ---

func TestValidateAwaitingInputState_NotAwaiting(t *testing.T) {
	sess := &Session{State: StateIdle, pty: fakepty.New()}
	err := sess.validateAwaitingInputState()
	if err == nil || !strings.Contains(err.Error(), "not awaiting input") {
		t.Errorf("expected 'not awaiting input' error, got %v", err)
	}
}

func TestValidateAwaitingInputState_NilPTY(t *testing.T) {
	sess := &Session{State: StateAwaitingInput}
	err := sess.validateAwaitingInputState()
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected 'not initialized' error, got %v", err)
	}
}

func TestValidateAwaitingInputState_Valid(t *testing.T) {
	sess := &Session{State: StateAwaitingInput, pty: fakepty.New()}
	err := sess.validateAwaitingInputState()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- Multiline delay cap test ---

func TestApplyMultilineDelay_CappedAt500ms(t *testing.T) {
	// With >10 lines, delay should be capped at 500ms (not exceed)
	clock := fakeclock.New(time.Now())
	sess := &Session{clock: clock}
	// Create a command with 20 newlines (20 * 50ms = 1000ms, should cap to 500ms)
	cmd := strings.Repeat("line\n", 20)
	sess.applyMultilineDelay(cmd)
	// Just verifying no crash; fakeclock.Sleep is a no-op
}

// --- ProvideInput validation with awaiting state ---

func TestProvideInput_NilPTY(t *testing.T) {
	sess := NewSession("test_provide_nil", "local")
	sess.State = StateAwaitingInput

	_, err := sess.ProvideInput("yes")
	if err == nil {
		t.Fatal("expected error for nil PTY")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("error = %q, want containing 'not initialized'", err.Error())
	}
}

// --- SFTPClient / TunnelManager validation tests ---

func TestSFTPClient_LocalMode(t *testing.T) {
	sess := &Session{Mode: "local"}
	_, err := sess.SFTPClient()
	if err == nil || !strings.Contains(err.Error(), "not available for local") {
		t.Errorf("expected 'not available for local' error, got %v", err)
	}
}

func TestSFTPClient_NilSSHClient(t *testing.T) {
	sess := &Session{Mode: "ssh"}
	_, err := sess.SFTPClient()
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected 'not initialized' error, got %v", err)
	}
}

func TestTunnelManager_LocalMode(t *testing.T) {
	sess := &Session{Mode: "local"}
	_, err := sess.TunnelManager()
	if err == nil || !strings.Contains(err.Error(), "not available for local") {
		t.Errorf("expected 'not available for local' error, got %v", err)
	}
}

func TestTunnelManager_NilSSHClient(t *testing.T) {
	sess := &Session{Mode: "ssh"}
	_, err := sess.TunnelManager()
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected 'not initialized' error, got %v", err)
	}
}
