package viaduct

import (
	"errors"
	"math/rand/v2"
	"time"
)

const (
	defaultRetryInitial = time.Second
	defaultRetryMax     = 30 * time.Second
)

// RetryPolicy sets how often a failing resource is retried. Each wait doubles
// from Initial up to Max, reduced by a random amount of up to half. The
// resource's lock is released while it waits. Errors wrapped with Permanent
// are not retried, and nor is a resource abandoned because it did not stop
// after its timeout. One that stops when its timeout passes is retried.
type RetryPolicy struct {
	// Attempts is how many times to try the resource, including the first.
	// Zero or one means it is not retried.
	Attempts int

	// Initial is the wait before the second attempt. The default is one
	// second, or Max if that is shorter.
	Initial time.Duration

	// Max is the longest wait between attempts. The default is 30 seconds.
	Max time.Duration

	// MaxElapsed stops retrying once this long has passed since the first
	// attempt started, including the next wait. Zero means no limit.
	MaxElapsed time.Duration
}

func (p RetryPolicy) withDefaults() RetryPolicy {
	if p.Initial <= 0 {
		p.Initial = defaultRetryInitial
	}

	if p.Max <= 0 {
		p.Max = defaultRetryMax
	}

	if p.Initial > p.Max {
		p.Initial = p.Max
	}

	return p
}

// backoff returns how long to wait after the given attempt, counting from 1.
func (p RetryPolicy) backoff(attempt int) time.Duration {
	p = p.withDefaults()

	d := p.Initial
	for i := 1; i < attempt && d < p.Max; i++ {
		d *= 2
	}

	if d > p.Max {
		d = p.Max
	}

	half := d / 2

	return half + rand.N(d-half+1)
}

// permanentError marks an error that retrying will not fix.
type permanentError struct {
	err error
}

func (e *permanentError) Error() string {
	return e.err.Error()
}

func (e *permanentError) Unwrap() error {
	return e.err
}

// Permanent marks an error as not worth retrying. It returns nil for a nil
// error.
func Permanent(err error) error {
	if err == nil {
		return nil
	}

	return &permanentError{err: err}
}

// IsPermanent reports whether an error was marked with Permanent.
func IsPermanent(err error) bool {
	var p *permanentError
	return errors.As(err, &p)
}

// skipError is a resource deciding it has nothing to do.
type skipError struct {
	reason string
}

func (e *skipError) Error() string {
	return "skipped: " + e.reason
}

// Skip is returned by a resource that has nothing to do. Its status is
// Skipped, and resources depending on it still run.
func Skip(reason string) error {
	return &skipError{reason: reason}
}

// IsSkip reports whether an error came from Skip.
func IsSkip(err error) bool {
	var s *skipError
	return errors.As(err, &s)
}
