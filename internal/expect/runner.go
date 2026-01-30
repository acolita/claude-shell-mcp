package expect

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// RunState tracks the execution state of a script.
type RunState struct {
	Script       *Script
	CurrentStep  int
	StepRepeats  map[int]int // Track repeat counts per step
	Completed    bool
	Aborted      bool
	Error        error
	StartedAt    time.Time
	CompletedAt  time.Time
}

// Runner executes expect scripts against session output.
type Runner struct {
	scripts []*Script
	mu      sync.RWMutex
}

// NewRunner creates a new expect runner with the given scripts.
func NewRunner(scripts []*Script) *Runner {
	return &Runner{
		scripts: scripts,
	}
}

// NewDefaultRunner creates a runner with all default scripts loaded.
func NewDefaultRunner() *Runner {
	return NewRunner(DefaultScripts())
}

// AddScript adds a script to the runner.
func (r *Runner) AddScript(script *Script) error {
	if err := script.Compile(); err != nil {
		return fmt.Errorf("failed to compile script %s: %w", script.Name, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.scripts = append(r.scripts, script)
	return nil
}

// FindScript finds a script that matches the given command.
func (r *Runner) FindScript(command string) *Script {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, script := range r.scripts {
		if script.MatchesCommand(command) {
			return script
		}
	}
	return nil
}

// Match represents a matched step with its response.
type Match struct {
	Script   *Script
	Step     *Step
	StepIdx  int
	Response string
	Action   Action
}

// MatchOutput finds matching steps for the given output.
// It returns the first matching step from any active script.
func (r *Runner) MatchOutput(state *RunState, output string) *Match {
	if state == nil || state.Script == nil || state.Completed || state.Aborted {
		return nil
	}

	script := state.Script

	// Try to match current step first
	if state.CurrentStep < len(script.Steps) {
		step := &script.Steps[state.CurrentStep]
		if step.CompiledPattern != nil && step.CompiledPattern.MatchString(output) {
			return &Match{
				Script:   script,
				Step:     step,
				StepIdx:  state.CurrentStep,
				Response: step.Response,
				Action:   step.Action,
			}
		}
	}

	// If current step is optional, try subsequent steps
	for i := state.CurrentStep; i < len(script.Steps); i++ {
		step := &script.Steps[i]

		// Skip non-optional steps unless they're the current one
		if i > state.CurrentStep && !script.Steps[state.CurrentStep].Optional {
			break
		}

		if step.CompiledPattern != nil && step.CompiledPattern.MatchString(output) {
			return &Match{
				Script:   script,
				Step:     step,
				StepIdx:  i,
				Response: step.Response,
				Action:   step.Action,
			}
		}
	}

	return nil
}

// AdvanceState advances the script state after a successful match.
func (r *Runner) AdvanceState(state *RunState, match *Match) {
	if state == nil || match == nil {
		return
	}

	step := match.Step

	// Handle repeating steps
	if step.Repeat {
		if state.StepRepeats == nil {
			state.StepRepeats = make(map[int]int)
		}
		state.StepRepeats[match.StepIdx]++

		// Check if max repeats reached
		if step.MaxRepeats > 0 && state.StepRepeats[match.StepIdx] >= step.MaxRepeats {
			state.CurrentStep = match.StepIdx + 1
			slog.Debug("expect step max repeats reached",
				slog.String("script", state.Script.Name),
				slog.String("step", step.Name),
				slog.Int("repeats", state.StepRepeats[match.StepIdx]),
			)
		}
		// Otherwise stay on the same step
	} else {
		// Move to next step
		state.CurrentStep = match.StepIdx + 1
	}

	// Check if script is complete
	if state.CurrentStep >= len(state.Script.Steps) {
		state.Completed = true
		state.CompletedAt = time.Now()
		slog.Debug("expect script completed",
			slog.String("script", state.Script.Name),
			slog.Duration("duration", state.CompletedAt.Sub(state.StartedAt)),
		)
	}
}

// StartScript initializes a new script execution state.
func (r *Runner) StartScript(script *Script) *RunState {
	return &RunState{
		Script:      script,
		CurrentStep: 0,
		StepRepeats: make(map[int]int),
		StartedAt:   time.Now(),
	}
}

// Manager manages expect script execution for multiple sessions.
type Manager struct {
	runner *Runner
	states map[string]*RunState // session_id -> state
	mu     sync.RWMutex
}

// NewManager creates a new expect manager.
func NewManager() *Manager {
	return &Manager{
		runner: NewDefaultRunner(),
		states: make(map[string]*RunState),
	}
}

// StartForSession starts script detection for a session's command.
func (m *Manager) StartForSession(sessionID, command string) *RunState {
	script := m.runner.FindScript(command)
	if script == nil {
		return nil
	}

	state := m.runner.StartScript(script)

	m.mu.Lock()
	m.states[sessionID] = state
	m.mu.Unlock()

	slog.Debug("started expect script for session",
		slog.String("session_id", sessionID),
		slog.String("script", script.Name),
		slog.String("command", command),
	)

	return state
}

// GetState returns the current expect state for a session.
func (m *Manager) GetState(sessionID string) *RunState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.states[sessionID]
}

// ProcessOutput processes session output and returns a response if a pattern matches.
func (m *Manager) ProcessOutput(sessionID, output string) *Match {
	m.mu.RLock()
	state := m.states[sessionID]
	m.mu.RUnlock()

	if state == nil {
		return nil
	}

	match := m.runner.MatchOutput(state, output)
	if match != nil {
		m.runner.AdvanceState(state, match)
	}

	return match
}

// EndSession removes the expect state for a session.
func (m *Manager) EndSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, sessionID)
}

// AddScript adds a custom script to the manager.
func (m *Manager) AddScript(script *Script) error {
	return m.runner.AddScript(script)
}

// WaitForStep waits for a specific step to be reached or times out.
func (m *Manager) WaitForStep(ctx context.Context, sessionID string, stepName string) error {
	state := m.GetState(sessionID)
	if state == nil {
		return fmt.Errorf("no expect state for session %s", sessionID)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			state = m.GetState(sessionID)
			if state == nil {
				return fmt.Errorf("expect state removed for session %s", sessionID)
			}

			if state.Completed {
				return nil
			}

			// Check if we've passed the step
			for i := 0; i < state.CurrentStep; i++ {
				if state.Script.Steps[i].Name == stepName {
					return nil
				}
			}
		}
	}
}
