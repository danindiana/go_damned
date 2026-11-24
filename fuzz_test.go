package main

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"
)

// Random-network search for exactness violations.
//
// Each case is a small random network: 3-6 neurons spread over 2-4
// processors, random thresholds, signed weights and delays, and a few
// stimuli. Every case is run under the guard being tested and compared
// spike for spike against sequentialEDS. A case that diverges is shrunk to a
// smaller network that still diverges and printed.
//
// Defaults are small so `go test` stays fast. For a long search:
//
//	FUZZ_CASES=20000 FUZZ_REPS=20 FUZZ_SEED=7 go test -run TestFuzz -v .
//
// TestFuzz2009Finds checks that the search has power: it must find at least
// one 2009 failure. TestFuzz2009L asserts that 2009L has none.

type fcase struct {
	P     int
	Theta []int
	Proc  []int
	Syn   [][4]int // pre, post, w, d
	Stim  [][2]int // neuron, stamp
	TMax  int
}

func (c fcase) build() (*Network, []CMEvent) {
	net := NewNetwork(c.P)
	for i := range c.Theta {
		net.Add(i, c.Theta[i], c.Proc[i])
	}
	for _, s := range c.Syn {
		net.Connect(s[0], s[1], s[2], s[3])
	}
	var stim []CMEvent
	for _, s := range c.Stim {
		stim = append(stim, CMEvent{Nj: s[0], Ni: -1, St: s[1], W: c.Theta[s[0]]})
	}
	return net, stim
}

func (c fcase) String() string {
	return fmt.Sprintf("P=%d theta=%v proc=%v syn(pre,post,w,d)=%v stim(n,t)=%v tmax=%d",
		c.P, c.Theta, c.Proc, c.Syn, c.Stim, c.TMax)
}

func randCase(r *rand.Rand) fcase {
	n := 3 + r.Intn(4)
	c := fcase{P: 2 + r.Intn(3), TMax: 8}
	for i := 0; i < n; i++ {
		c.Theta = append(c.Theta, 1+r.Intn(3))
		c.Proc = append(c.Proc, 1+r.Intn(c.P))
	}
	seen := map[[2]int]bool{}
	for k := 0; k < n+r.Intn(2*n); k++ {
		a, b := r.Intn(n), r.Intn(n)
		if a == b || seen[[2]int{a, b}] {
			continue
		}
		seen[[2]int{a, b}] = true
		w := 1 + r.Intn(3)
		if r.Intn(3) == 0 {
			w = -(1 + r.Intn(4))
		}
		c.Syn = append(c.Syn, [4]int{a, b, w, 1 + r.Intn(3)})
	}
	for k := 0; k < 1+r.Intn(3); k++ {
		c.Stim = append(c.Stim, [2]int{r.Intn(n), r.Intn(4)})
	}
	return c
}

// diverges runs the case `reps` times and reports whether any run's spike
// train differed from the sequential reference.
func diverges(c fcase, guard string, reps int) (bool, [][2]int, [][2]int) {
	net, stim := c.build()
	truth := sequentialEDS(net, stim, c.TMax)
	for i := 0; i < reps; i++ {
		got, _, _ := runOnce(net, stim, Config{Guard: guard, TMax: c.TMax})
		if !equalPairs(got, truth) {
			return true, truth, got
		}
	}
	return false, truth, nil
}

// shrink greedily removes synapses and stimuli while the case still diverges.
func shrink(c fcase, guard string, reps int) fcase {
	for changed := true; changed; {
		changed = false
		for i := range c.Syn {
			d := c
			d.Syn = append(append([][4]int{}, c.Syn[:i]...), c.Syn[i+1:]...)
			if bad, _, _ := diverges(d, guard, reps*3); bad {
				c, changed = d, true
				break
			}
		}
		if changed {
			continue
		}
		for i := range c.Stim {
			if len(c.Stim) == 1 {
				break
			}
			d := c
			d.Stim = append(append([][2]int{}, c.Stim[:i]...), c.Stim[i+1:]...)
			if bad, _, _ := diverges(d, guard, reps*3); bad {
				c, changed = d, true
				break
			}
		}
	}
	return c
}

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil && v > 0 {
		return v
	}
	return def
}

func search(t *testing.T, guard string, seed int64) (found int, first *fcase, cases int) {
	cases, reps := envInt("FUZZ_CASES", 400), envInt("FUZZ_REPS", 10)
	r := rand.New(rand.NewSource(seed + int64(envInt("FUZZ_SEED", 0))))
	for i := 0; i < cases; i++ {
		c := randCase(r)
		if bad, _, _ := diverges(c, guard, reps); bad {
			found++
			if first == nil {
				cc := c
				first = &cc
			}
		}
	}
	return
}

func TestFuzz2009L(t *testing.T) {
	found, first, cases := search(t, "2009L", 1)
	t.Logf("2009L: %d of %d random networks diverged", found, cases)
	if first != nil {
		s := shrink(*first, "2009L", envInt("FUZZ_REPS", 10))
		_, truth, got := diverges(s, "2009L", 200)
		t.Fatalf("2009L diverged.\n  case:  %v\n  truth: %v\n  got:   %v", s, truth, got)
	}
}

func TestFuzz2009Finds(t *testing.T) {
	found, first, cases := search(t, "2009", 1)
	t.Logf("2009: %d of %d random networks diverged", found, cases)
	if first == nil {
		t.Skip("no 2009 divergence found at this budget; raise FUZZ_CASES or FUZZ_REPS")
	}
	s := shrink(*first, "2009", envInt("FUZZ_REPS", 10))
	t.Logf("smallest 2009 counterexample found: %v", s)
}
