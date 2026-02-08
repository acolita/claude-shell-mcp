package ports

// ServerFormData holds the result of a server configuration form.
type ServerFormData struct {
	Name            string
	Host            string
	Port            int
	User            string
	KeyPath         string
	AuthType        string
	SudoPasswordEnv string
	Confirmed       bool
}

// DialogProvider abstracts interactive user dialogs.
// Implementations may use TUI forms, native OS dialogs, or test fakes.
type DialogProvider interface {
	// ServerConfigForm shows a form to confirm/edit server configuration.
	// Pre-filled values come from the input data; the user can modify them.
	// Returns the final form data with Confirmed=true if the user accepted.
	ServerConfigForm(prefill ServerFormData) (ServerFormData, error)
}
