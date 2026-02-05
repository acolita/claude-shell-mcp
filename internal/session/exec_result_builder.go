package session

import (
	"strings"

	"github.com/acolita/claude-shell-mcp/internal/prompt"
)

// execContext holds common context for building ExecResult objects.
type execContext struct {
	commandID   string
	startMarker string
	endMarker   string
	command     string
}

// newExecContext creates a new execution context.
func newExecContext(cmdID, startMarker, endMarker, command string) *execContext {
	return &execContext{
		commandID:   cmdID,
		startMarker: startMarker,
		endMarker:   endMarker,
		command:     command,
	}
}

// buildCompletedResult creates a completed ExecResult.
func (s *Session) buildCompletedResult(ctx *execContext, exitCode int, cwd string) *ExecResult {
	asyncOutput, stdout := s.parseMarkedOutput(s.outputBuffer.String(), ctx.startMarker, ctx.endMarker, ctx.command)
	return &ExecResult{
		Status:      "completed",
		ExitCode:    &exitCode,
		Stdout:      stdout,
		AsyncOutput: asyncOutput,
		CommandID:   ctx.commandID,
		Cwd:         cwd,
	}
}

// buildTimeoutResult creates a timeout ExecResult.
func (s *Session) buildTimeoutResult(ctx *execContext) *ExecResult {
	asyncOutput, stdout := s.parseMarkedOutput(s.outputBuffer.String(), ctx.startMarker, ctx.endMarker, ctx.command)
	return &ExecResult{
		Status:      "timeout",
		Stdout:      stdout,
		AsyncOutput: asyncOutput,
		CommandID:   ctx.commandID,
	}
}

// buildPeakTTYResult creates an awaiting_input ExecResult for peak-tty signal.
func (s *Session) buildPeakTTYResult(ctx *execContext, output string) *ExecResult {
	asyncOutput, stdout := s.parseMarkedOutput(output, ctx.startMarker, ctx.endMarker, ctx.command)
	cleanStdout := strings.ReplaceAll(stdout, "\x00", "")
	return &ExecResult{
		Status:        "awaiting_input",
		Stdout:        cleanStdout,
		AsyncOutput:   asyncOutput,
		CommandID:     ctx.commandID,
		PromptType:    "interactive",
		PromptText:    "",
		ContextBuffer: stripANSI(strings.ReplaceAll(output, "\x00", "")),
		Hint:          hintPeakTTYWaiting,
	}
}

// buildPromptResult creates an awaiting_input ExecResult for a detected prompt.
func (s *Session) buildPromptResult(ctx *execContext, output string, detection *prompt.Detection) *ExecResult {
	asyncOutput, stdout := s.parseMarkedOutput(output, ctx.startMarker, ctx.endMarker, ctx.command)
	return &ExecResult{
		Status:        "awaiting_input",
		Stdout:        stdout,
		AsyncOutput:   asyncOutput,
		CommandID:     ctx.commandID,
		PromptType:    string(detection.Pattern.Type),
		PromptText:    detection.MatchedText,
		ContextBuffer: detection.ContextBuffer,
		MaskInput:     detection.Pattern.MaskInput,
		Hint:          detection.Hint(),
	}
}

// checkForCompletion checks if command completed and returns result if found.
func (s *Session) checkForCompletion(ctx *execContext) (*ExecResult, bool) {
	output := s.outputBuffer.String()
	exitCode, found := s.extractExitCodeWithMarker(output, ctx.endMarker)
	if !found {
		return nil, false
	}
	s.State = StateIdle
	s.updateCwd()
	return s.buildCompletedResult(ctx, exitCode, s.Cwd), true
}

// checkForPeakTTYSignal checks for peak-tty signal and returns result if found.
func (s *Session) checkForPeakTTYSignal(ctx *execContext) (*ExecResult, bool) {
	output := s.outputBuffer.String()
	if !containsPeakTTYSignal(output) {
		return nil, false
	}
	s.State = StateAwaitingInput
	return s.buildPeakTTYResult(ctx, output), true
}

// checkForPasswordPrompt checks for password prompt and returns result if found.
func (s *Session) checkForPasswordPrompt(ctx *execContext, strippedOutput string) (*ExecResult, bool) {
	detection := s.promptDetector.Detect(strippedOutput)
	if detection == nil || detection.Pattern.Type != "password" {
		return nil, false
	}
	s.State = StateAwaitingInput
	s.pendingPrompt = detection
	output := s.outputBuffer.String()
	return s.buildPromptResult(ctx, output, detection), true
}

// checkForInteractivePrompt checks for any interactive prompt and returns result if found.
func (s *Session) checkForInteractivePrompt(ctx *execContext, output string) (*ExecResult, bool) {
	detection := s.promptDetector.Detect(output)
	if detection == nil {
		return nil, false
	}
	s.State = StateAwaitingInput
	s.pendingPrompt = detection
	return s.buildPromptResult(ctx, output, detection), true
}
