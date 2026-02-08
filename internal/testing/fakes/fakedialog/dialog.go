// Package fakedialog provides a test fake for ports.DialogProvider.
package fakedialog

import "github.com/acolita/claude-shell-mcp/internal/ports"

// Provider is a controllable fake DialogProvider for testing.
type Provider struct {
	// Result is the form data returned by ServerConfigForm.
	Result ports.ServerFormData
	// Err is the error returned by ServerConfigForm.
	Err error
	// Called tracks whether ServerConfigForm was invoked.
	Called bool
	// ReceivedPrefill captures the prefill data passed to ServerConfigForm.
	ReceivedPrefill ports.ServerFormData
}

// New returns a new fake dialog provider.
func New() *Provider {
	return &Provider{}
}

// ServerConfigForm returns the pre-configured Result and Err.
func (p *Provider) ServerConfigForm(prefill ports.ServerFormData) (ports.ServerFormData, error) {
	p.Called = true
	p.ReceivedPrefill = prefill
	if p.Err != nil {
		return prefill, p.Err
	}
	return p.Result, nil
}
