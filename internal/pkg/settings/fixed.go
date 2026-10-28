package settings

// Class says why a setting can only be set in the server's environment.
type Class string

const (
	// ClassBootstrap is needed before the saved settings can be read, or
	// protects the place they are kept.
	ClassBootstrap Class = "bootstrap"
	// ClassGuardrail limits what an administrator can do, so an administrator
	// must not be able to change it from the UI.
	ClassGuardrail Class = "guardrail"
)

// Fixed is a setting the Settings page shows and never changes.
type Fixed struct {
	Name   string
	Class  Class
	Reason string
	// Secret settings are shown only as set or not set.
	Secret bool
}
