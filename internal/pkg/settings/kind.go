package settings

// Kind is what a setting's value is read as, which decides how it is checked
// before it is saved.
type Kind string

const (
	// KindText is free text on one line.
	KindText Kind = "text"
	// KindInteger is a whole number between a definition's Min and Max.
	KindInteger Kind = "integer"
	// KindDuration is a Go duration, such as 30s or 72h, between a
	// definition's MinDuration and MaxDuration.
	KindDuration Kind = "duration"
	// KindBoolean is true or false.
	KindBoolean Kind = "boolean"
	// KindRatio is a fraction from 0 to 1.
	KindRatio Kind = "ratio"
	// KindURL is an absolute http or https address with no credentials in it.
	KindURL Kind = "url"
	// KindHostPort is a host and a port, such as collector:4318.
	KindHostPort Kind = "host_port"
	// KindDays is a whole number of days; 0 means off.
	KindDays Kind = "days"
)
