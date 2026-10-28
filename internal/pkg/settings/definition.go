package settings

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// maxTextLength bounds a text value. Nothing a server setting names — a
// host, an address, a claim — is longer, and a row is read at every boot.
const maxTextLength = 1000

// Definition is one setting that can be set from the Settings page.
//
// Name is the environment variable the setting has always been read from, and
// stays its name: the environment still wins where it is set, so one name
// says what is being set wherever it is.
type Definition struct {
	Name  string
	Group Group
	Label string
	Help  string
	// Default describes what the server uses when nothing is set.
	Default string

	Kind    Kind
	Secret  bool
	Applies Applies

	// Min and Max bound KindInteger and KindDays.
	Min, Max int64
	// MinDuration and MaxDuration bound KindDuration.
	MinDuration, MaxDuration time.Duration
}

// Validate reports why value cannot be saved for this setting.
//
// The bounds are the ones the server applies when it reads the setting, so a
// value the page accepts is one the server uses rather than one it warns
// about and replaces with its default at the next start.
func (d Definition) Validate(value string) error {
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("%s has spaces around it", d.Label)
	}
	if value == "" {
		return fmt.Errorf("%s is empty; clear it instead to use the default", d.Label)
	}
	if len(value) > maxTextLength || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return fmt.Errorf("%s must be one line of at most %d characters", d.Label, maxTextLength)
	}
	switch d.Kind {
	case KindInteger, KindDays:
		return d.validateInteger(value)
	case KindDuration:
		return d.validateDuration(value)
	case KindBoolean:
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("%s is true or false", d.Label)
		}
	case KindRatio:
		ratio, err := strconv.ParseFloat(value, 64)
		if err != nil || ratio < 0 || ratio > 1 {
			return fmt.Errorf("%s is a number from 0 to 1", d.Label)
		}
	case KindURL:
		return d.validateURL(value)
	case KindHostPort:
		if _, port, err := net.SplitHostPort(value); err != nil || port == "" {
			return fmt.Errorf("%s is a host and a port, such as collector:4318", d.Label)
		}
	}
	return nil
}

func (d Definition) validateInteger(value string) error {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < d.Min || n > d.Max {
		return fmt.Errorf("%s is a whole number from %d to %d", d.Label, d.Min, d.Max)
	}
	return nil
}

func (d Definition) validateDuration(value string) error {
	duration, err := time.ParseDuration(value)
	if err != nil || duration < d.MinDuration || duration > d.MaxDuration {
		return fmt.Errorf("%s is a duration from %s to %s, such as %s", d.Label, d.MinDuration, d.MaxDuration, d.Default)
	}
	return nil
}

func (d Definition) validateURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return fmt.Errorf("%s is an http or https address with a host and no credentials", d.Label)
	}
	return nil
}
