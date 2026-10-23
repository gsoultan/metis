package main

import (
	"fmt"
	"time"
)

// apiError is a response the server refused, kept by status so the report can
// tell shedding (429, 503) from failure (500) from a refusal of the request.
type apiError struct {
	status     int
	body       string
	retryAfter time.Duration
}

func (e *apiError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.status, e.body)
}
