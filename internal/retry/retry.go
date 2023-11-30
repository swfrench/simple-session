// Package retry implements retries with jittered exponential backoff.
package retry

import (
	"errors"
	"fmt"
	"math/rand"
	"time"
)

var (
	// ErrInvalidPolicyParam indicates that one or more Policy parameters are
	// invalid (e.g., fall outside accepted intervals).
	ErrInvalidPolicyParam = errors.New("invalid policy param")
	// ErrAborted indicates that the work function provided to Do invoked the
	// Abort callback indicating a non-retryable error.
	ErrAborted = errors.New("aborted")
	// ErrExhausted indicates that the work function provided to Do exhausted
	// the provided attempt budget without succeeding.
	ErrExhausted = errors.New("too many attempts")
)

// Policy represents an abstract retry policy, which can be used to execute a
// retryable work function.
type Policy interface {
	// Do invokes fn up to n times (i.e., the attempt budget).
	Do(fn WorkFn, n int) error
}

// Backoff is a Policy implementing jittered exponential backoff. Multiple
// goroutines may use a given Backoff instance concurrently.
type Backoff struct {
	// Base is the initial delay between attempts.
	Base time.Duration
	// Growth is the multiplicative growth factor used to increase the delay on
	// successive attempts, and must be greater than or equal to 1.
	Growth float64
	// Jitter is the fractional amplitude of the random jitter applied to the
	// delay each time Do sleeps prior to the next attempt, and must be in the
	// interval [0, 1].
	Jitter float64
	sleep  func(time.Duration) // overidden in tests
}

func (b *Backoff) validate() error {
	if b.Growth < 1.0 {
		return fmt.Errorf("delay growth factor is less than 1: %w", ErrInvalidPolicyParam)
	}
	if b.Jitter < 0.0 {
		return fmt.Errorf("delay jitter amplitude is negative: %w", ErrInvalidPolicyParam)
	}
	if b.Jitter > 1.0 {
		return fmt.Errorf("delay jitter amplitude is greater than 1: %w", ErrInvalidPolicyParam)
	}
	return nil
}

// WorkFn represents the retryable work provided to a Policy for execution
// (i.e., passed to Do). The function must report attempt outcome status via the
// provided RetryContext.
type WorkFn func(*RetryContext)

// RetryContext is passed to a WorkFn provided to a given Policy via Do, and is
// used to report "terminal" attempt outcomes.
// Note: A retryable error need not be explicitly reported.
type RetryContext struct {
	// Done should be invoked when the WorkFn has successfully completed its
	// work, and need not be retried.
	Done func()
	// Abort should be invoked when the WorkFn encounters a non-retryable error,
	// and thus should not be retried.
	Abort func()
}

// scale scales the duration d by f, truncated to integer nanoseconds.
func scale(d time.Duration, f float64) time.Duration {
	return time.Duration(float64(d.Nanoseconds())*f) * time.Nanosecond
}

// Do invokes the provided WorkFn up to n times according to the configured
// backoff policy.
func (b Backoff) Do(fn WorkFn, n int) error {
	if err := b.validate(); err != nil {
		return err
	}
	sleep := b.sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	onCall := func(called *bool) func() { return func() { *called = true } }
	var done bool
	var aborted bool
	rctx := RetryContext{
		Done:  onCall(&done),
		Abort: onCall(&aborted),
	}
	d := b.Base
	for i := 1; i <= n; i++ {
		fn(&rctx)
		if done {
			return nil
		}
		if aborted {
			return ErrAborted
		}
		if i < n {
			// Note: Jitter is actually over the interval [1-J, 1+J).
			sleep(scale(d, 1.0+b.Jitter*(2*rand.Float64()-1.0)))
			d = scale(d, b.Growth)
		}
	}
	return ErrExhausted
}
