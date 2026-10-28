// Package factories builds records for tests. Each function fills in valid defaults, lets the test override only
// what it cares about, saves through the real store, and fails the test on
// any error, so a test reads as its arrangement and nothing else.
//
//	post := factories.Post(t, store)
//	draft := factories.Post(t, store, func(p *models.Post) { p.Published = false })
//
// Write one function per model, in a file of its own beside this one.
//
// Unique columns (emails, names, tokens) come from Sequence, so two records
// made in one test never collide.
package factories

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// T is the part of *testing.T the factories need, so they also work with
// Ginkgo's GinkgoT().
type T interface {
	Helper()
	Fatal(args ...any)
}

var counters sync.Map // prefix -> *int64

// Sequence returns prefix followed by a number that is new each call:
// Sequence("user") is "user1", then "user2".
func Sequence(prefix string) string {
	c, _ := counters.LoadOrStore(prefix, new(int64))
	return fmt.Sprintf("%s%d", prefix, atomic.AddInt64(c.(*int64), 1))
}
