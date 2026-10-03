package contracts

// ActivationOption adjusts one activation of a step inside an ad-hoc
// sub-process.
//
// Variadic, as a migration's options are, so that the ordinary call — start
// this step — stays the short one.
type ActivationOption func(*ActivationOptions)

// ActivationOptions is what an activation was told beyond which step to start.
//
// An activation is a modelled option, so its reason is optional; it is kept
// when given.
type ActivationOptions struct {
	// Reason is why the step was started, in the words of whoever started it.
	Reason string
}

// WithActivationReason records why the step was started.
func WithActivationReason(reason string) ActivationOption {
	return func(o *ActivationOptions) { o.Reason = reason }
}

// ApplyActivationOptions folds a list of options into one value.
func ApplyActivationOptions(opts []ActivationOption) ActivationOptions {
	var resolved ActivationOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&resolved)
		}
	}
	return resolved
}
