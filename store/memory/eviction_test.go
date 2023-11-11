package memory

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestEvictionQueue(t *testing.T) {
	now := time.Now()
	type insert struct {
		key string
		exp time.Time
	}
	testCases := []struct {
		name     string
		inserts  []insert
		wantPeek []string
		wantPop  []string
	}{
		{
			name: "in order",
			inserts: []insert{
				{key: "a", exp: now.Add(time.Minute)},
				{key: "b", exp: now.Add(2 * time.Minute)},
				{key: "c", exp: now.Add(3 * time.Minute)},
			},
			wantPeek: []string{"a", "a", "a"},
			wantPop:  []string{"a", "b", "c"},
		},
		{
			name: "out of order",
			inserts: []insert{
				{key: "b", exp: now.Add(2 * time.Minute)},
				{key: "c", exp: now.Add(3 * time.Minute)},
				{key: "a", exp: now.Add(time.Minute)},
			},
			wantPeek: []string{"b", "b", "a"},
			wantPop:  []string{"a", "b", "c"},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			eq := newEvictionQueue()
			for i := range tc.inserts {
				eq.Push(tc.inserts[i].key, tc.inserts[i].exp)
				if got, want := eq.Peek().key, tc.wantPeek[i]; got != want {
					t.Errorf("Peek().key = %q, want %q (insert: %d)", got, want, i)
				}
			}
			var keys []string
			for eq.Len() > 0 {
				keys = append(keys, eq.Pop().key)
			}
			if diff := cmp.Diff(tc.wantPop, keys); diff != "" {
				t.Errorf("Pop() returned incorrect key sequence (+got, -want):\n%s", diff)
			}
		})
	}
}
