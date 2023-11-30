package retry

import (
	"errors"
	"testing"
	"time"
)

func TestInvalidPolicyParam(t *testing.T) {
	testCases := []struct {
		name   string
		policy Policy
	}{
		{
			name:   "backoff: growth factor too small",
			policy: &Backoff{Base: 100 * time.Millisecond, Growth: 0.9, Jitter: 0.1},
		},
		{
			name:   "backoff: jitter amplitude too large",
			policy: &Backoff{Base: 100 * time.Millisecond, Growth: 1.2, Jitter: 1.5},
		},
		{
			name:   "backoff: jitter amplitude negative",
			policy: &Backoff{Base: 100 * time.Millisecond, Growth: 1.2, Jitter: -0.1},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fn := func(*RetryContext) {
				t.Error("Do(fn, n) unexpectedly invoked fn")
			}
			if got, want := tc.policy.Do(fn, 3), ErrInvalidPolicyParam; !errors.Is(got, want) {
				t.Errorf("Policy ctor did not produce the expected error status: got: %v, want: %v", got, want)
			}
		})
	}
}

type countingWorker struct {
	attempts int
	fnInner  func(rc *RetryContext)
}

func (cw *countingWorker) fn(rc *RetryContext) {
	cw.attempts++
	cw.fnInner(rc)
}

func TestBackoffBasic(t *testing.T) {
	backoff := &Backoff{Base: 100 * time.Millisecond, Growth: 1.1, Jitter: 0.1}
	testCases := []struct {
		name     string
		fn       func(*RetryContext)
		budget   int
		attempts int
		err      error
	}{
		{
			name:     "exhausted",
			fn:       func(rc *RetryContext) {},
			budget:   3,
			attempts: 3,
			err:      ErrExhausted,
		},
		{
			name:     "aborted",
			fn:       func(rc *RetryContext) { rc.Abort() },
			budget:   3,
			attempts: 1,
			err:      ErrAborted,
		},
		{
			name:     "succeeds",
			fn:       func(rc *RetryContext) { rc.Done() },
			budget:   3,
			attempts: 1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			w := &countingWorker{fnInner: tc.fn}
			err := backoff.Do(w.fn, tc.budget)
			if gotErr, wantErr := err != nil, tc.err != nil; gotErr != wantErr {
				t.Errorf("Do(fn, %d) returned unexpected error state: gotErr: %t, wantErr: %t", tc.budget, gotErr, wantErr)
			} else if tc.err != nil && !errors.Is(err, tc.err) {
				t.Errorf("Do(fn, %d) returned incorrect error type: got: %v, want: %v", tc.budget, err, tc.err)
			}
			if got, want := w.attempts, tc.attempts; got != want {
				t.Errorf("Do(fn, %d) invoked fn an incorrect number of times: got: %d, want: %d", tc.budget, got, want)
			}
		})
	}
}

type interval struct {
	min time.Duration
	max time.Duration
}

func (i interval) contains(d time.Duration) bool {
	return d >= i.min && d <= i.max
}

func TestBackoffDelays(t *testing.T) {
	testCases := []struct {
		name    string
		backoff *Backoff
		budget  int
		delays  []interval
	}{
		{
			name:    "no growth - no jitter",
			backoff: &Backoff{Base: 100 * time.Millisecond, Growth: 1.0, Jitter: 0.0},
			budget:  4,
			delays: []interval{
				{min: 100 * time.Millisecond, max: 100 * time.Millisecond},
				{min: 100 * time.Millisecond, max: 100 * time.Millisecond},
				{min: 100 * time.Millisecond, max: 100 * time.Millisecond},
			},
		},
		{
			name:    "growth - no jitter",
			backoff: &Backoff{Base: 100 * time.Millisecond, Growth: 1.2, Jitter: 0.0},
			budget:  4,
			delays: []interval{
				{min: 100 * time.Millisecond, max: 100 * time.Millisecond},
				{min: 120 * time.Millisecond, max: 120 * time.Millisecond},
				{min: 144 * time.Millisecond, max: 144 * time.Millisecond},
			},
		},
		{
			name:    "no growth - jitter",
			backoff: &Backoff{Base: 100 * time.Millisecond, Growth: 1.0, Jitter: 0.1},
			budget:  4,
			delays: []interval{
				{min: 90 * time.Millisecond, max: 110 * time.Millisecond},
				{min: 90 * time.Millisecond, max: 110 * time.Millisecond},
				{min: 90 * time.Millisecond, max: 110 * time.Millisecond},
			},
		},
		{
			name:    "growth - jitter",
			backoff: &Backoff{Base: 100 * time.Millisecond, Growth: 1.2, Jitter: 0.1},
			budget:  4,
			delays: []interval{
				{min: 90 * time.Millisecond, max: 110 * time.Millisecond},
				{min: 108 * time.Millisecond, max: 132 * time.Millisecond},
				{min: 129 * time.Millisecond, max: 159 * time.Millisecond},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Note: This is why we are testing _in_ package retry (i.e., we are
			// using a private test helper to intercept sleep).
			var delays []time.Duration
			tc.backoff.sleep = func(d time.Duration) {
				delays = append(delays, d)
			}
			if got, want := tc.backoff.Do(func(rc *RetryContext) {}, tc.budget), ErrExhausted; !errors.Is(got, want) {
				t.Errorf("Do(fn, %d) returned unexpected error status: got: %d, want: %d", tc.budget, got, want)
			}
			if got, want := len(delays), len(tc.delays); got != want {
				t.Fatalf("Do(fn, %d) invoked sleep an incorrect number of times: got: %d, want: %d", tc.budget, got, want)
			}
			for i, d := range delays {
				if !tc.delays[i].contains(d) {
					t.Errorf("Do(fn, %d) invoked sleep with an unexpected duration: got: %v, want in interval: %v", tc.budget, d, tc.delays[i])
				}
			}
		})
	}
}
