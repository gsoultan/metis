package main

import (
	"fmt"
	"io"
)

// reportWriter keeps the first write error, so the report reads as a list of
// lines rather than a list of error checks, and the caller still learns that
// stdout went away part way through.
type reportWriter struct {
	w   io.Writer
	err error
}

func (r *reportWriter) printf(format string, args ...any) {
	if r.err == nil {
		_, r.err = fmt.Fprintf(r.w, format, args...)
	}
}

func (r *reportWriter) println(text string) {
	r.printf("%s\n", text)
}
