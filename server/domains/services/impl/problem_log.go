package impl

import (
	"github.com/rs/zerolog"
)

// maxRememberedProblems bounds what a problemLog remembers. Past it, it
// starts again: that costs a line said twice, not memory.
const maxRememberedProblems = 32

// problemLog says each problem a RabbitMQ bridge or consumer runs into once,
// until it is working again.
//
// A broker that stays down, an exchange that stays missing, a queue that
// exists with other arguments: each fails the same way at every attempt, and
// a line per attempt buries the one worth reading. The first time a cause is
// seen it is logged at error, and after that at debug; once the bridge or
// consumer is working again it is all forgotten, so the next outage is said
// afresh.
type problemLog struct {
	logger *zerolog.Logger
	said   map[string]struct{}
}

// event starts the line for message and err, at error level if the pair is
// new and at debug if it has been said since the last time things worked.
// The caller adds its fields and ends it with Msg(message).
func (p *problemLog) event(message string, err error) *zerolog.Event {
	return p.logger.WithLevel(p.level(message + ": " + err.Error())).Err(err)
}

func (p *problemLog) level(cause string) zerolog.Level {
	if _, said := p.said[cause]; said {
		return zerolog.DebugLevel
	}
	if p.said == nil || len(p.said) >= maxRememberedProblems {
		p.said = map[string]struct{}{}
	}
	p.said[cause] = struct{}{}
	return zerolog.ErrorLevel
}

// working forgets what has been said: the next problem is news.
func (p *problemLog) working() {
	clear(p.said)
}
