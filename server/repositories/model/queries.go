package model

import (
	"github.com/gsoultan/storm"
)

// Queries is every statement written in SQL rather than built from the model,
// for the generator to PREPARE against the model and give a typed scanner.
//
// A statement declared here and not listed is refused when it runs: storm only
// runs the text it was shown at generate time, which is what stops a string
// put together at run time from passing for one of these. A statement listed
// here whose columns drift from the model fails `make generate`, naming the
// column, instead of failing a request.
//
// Each lives here rather than beside its repository because the generator
// reads this package and nothing else; the repository says why it is SQL.
func Queries() []storm.RawDecl {
	return nil
}
