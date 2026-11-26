package grule

// The limits on what one import may hold. A rule file is untrusted input, and
// each of these bounds the memory or the time a hostile one can cost before it
// is refused with a message saying which limit it reached.
const (
	// MaxSourceBytes is the largest file accepted, and MaxTotalBytes the most
	// across the files of one import.
	MaxSourceBytes = 1 << 20
	MaxTotalBytes  = 4 << 20

	// MaxFiles is how many files one import may hold.
	MaxFiles = 50

	// MaxRules is how many rules one import may hold.
	MaxRules = 2000

	// MaxStatements is how many actions one rule's `then` may hold.
	MaxStatements = 200

	// MaxDepth is how deeply an expression may nest, in GRL and in JSON,
	// counting each operator of a chain such as `a + b + c` as a level. The
	// parser and everything after it recurse, so this bounds their stacks.
	MaxDepth = 100
)
