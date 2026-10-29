package settings

// Group is the heading a setting is shown under.
type Group string

const (
	GroupEmail         Group = "email"
	GroupSignIn        Group = "sign_in"
	GroupRetention     Group = "retention"
	GroupWorkers       Group = "workers"
	GroupLimits        Group = "limits"
	GroupCaches        Group = "caches"
	GroupObservability Group = "observability"
)
