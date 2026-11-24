package main

// ---------------------------------------------------------------------------
// Substrate witness: the group word problem on a constant-size spiking network.
//
// Barrington (1986): the word problem for any NON-SOLVABLE group is NC1-complete
// under AC0 reductions; for solvable groups it lies in ACC0. A device that tracks
// an S5 product over an input of length n therefore computes an NC1-complete
// function. If it does so with a FIXED number of neurons and synapses -- no
// dependence on n -- then the capability came from TEMPORAL state, not from
// spatial parallelism. That is the claim this scenario makes executable.
//
// Control: Z120. Same order (120), same generator count, therefore the *identical*
// neuron and synapse budget -- but abelian, hence solvable, hence ACC0. Any
// difference between the two runs cannot be attributed to network size.
//
// ---------------------------------------------------------------------------
// ENCODING
//
// Neuron model available (from Damned.go): integer potential, no leak, fires when
// pot >= theta, resets to 0 on fire, at most one fire per timestep, and at equal
// timestamps ALL inhibitory impacts are applied before excitatory ones.
//
// The no-leak property is the whole difficulty: a coincidence detector with two
// sparse subthreshold inputs accumulates across steps and eventually fires
// spuriously. This is solved without touching the neuron model, by flushing the
// conjunction layer every step and exactly cancelling the flush echo.
//
// Layers:
//   s_sigma       one-hot state, |G| neurons, theta=2
//   c_{pi,g}      conjunction, |G|*|S| neurons, theta=2
//   in_g          input line per generator, |S| neurons, theta=1
//   FL            flush clock, theta=1
//   IH            cancel clock, theta=1
//
// Step k occupies ticks [2k, 2k+2). Schedule:
//   t=2k    s_sigma fires (current state); in_{g_k} fires (current symbol)
//   t=2k+1  c_{sigma,g} sees +1 (state) +1 (input) = 2 -> FIRES.
//           Off-diagonal c neurons hold a residual +1.
//           FL fires (stimulated), targeting the whole conjunction layer.
//   t=2k+2  s_{g.sigma} receives +2 from the winning c -> FIRES. Next state.
//           FL's +2 reaches EVERY c: each fires (residual 0 or 1, +2 >= theta)
//           and resets to 0. Conjunction layer is clean for step k+1.
//           IH fires (stimulated).
//   t=2k+3  Flush echo: every s receives +2 from each of its |S| incoming
//           conjunctions = +2|S|, a CONSTANT because every state has exactly
//           one predecessor per generator. IH delivers exactly -2|S| at the same
//           tick; inhibition sorts first, so each s goes 0 -> -2|S| -> 0 and
//           never crosses threshold. Net zero.
//
// That last cancellation is the point of contact with the guard. It is an
// inhibitory pulse that must arrive before an excitatory one carried on a
// different processor. If a processor commits the excitatory sum without proof
// that the inhibition cannot still be in flight, every state neuron fires at
// once and the tracked group element is destroyed. Guard soundness and algebraic
// correctness are the same property here.
//
// Cost: neurons and synapses are O(|G|*|S|), independent of n. Depth is 2n.
// ---------------------------------------------------------------------------

import (
	"fmt"
	"math/rand"
)

// group is a finite group presented as a transition table over generators.
type group struct {
	name  string
	order int
	gens  int
	// trans[g][pi] = index of  g . pi   (left multiplication)
	trans [][]int
	label func(int) string
}

// ---- S5: symmetric group on 5 points. Non-solvable (contains simple A5). ----

func permCompose(g, p [5]int) [5]int { // (g . p)(i) = g(p(i))
	var r [5]int
	for i := 0; i < 5; i++ {
		r[i] = g[p[i]]
	}
	return r
}

func buildS5() *group {
	// enumerate all 120 permutations of 5 points
	var elems [][5]int
	var rec func(cur []int, used [5]bool)
	rec = func(cur []int, used [5]bool) {
		if len(cur) == 5 {
			var p [5]int
			copy(p[:], cur)
			elems = append(elems, p)
			return
		}
		for v := 0; v < 5; v++ {
			if !used[v] {
				used[v] = true
				rec(append(cur, v), used)
				used[v] = false
			}
		}
	}
	rec(nil, [5]bool{})

	idx := map[[5]int]int{}
	for i, p := range elems {
		idx[p] = i
	}

	// generators: transposition (0 1) and the 5-cycle (0 1 2 3 4)
	gens := [][5]int{
		{1, 0, 2, 3, 4},
		{1, 2, 3, 4, 0},
	}

	trans := make([][]int, len(gens))
	for gi, g := range gens {
		trans[gi] = make([]int, len(elems))
		for pi, p := range elems {
			trans[gi][pi] = idx[permCompose(g, p)]
		}
	}
	return &group{
		name: "S5", order: len(elems), gens: len(gens), trans: trans,
		label: func(i int) string { return fmt.Sprintf("%v", elems[i]) },
	}
}

// ---- Z120: cyclic group of order 120. Abelian, hence solvable. Control. ----

func buildZ120() *group {
	const n = 120
	gens := []int{1, 7}
	trans := make([][]int, len(gens))
	for gi, g := range gens {
		trans[gi] = make([]int, n)
		for x := 0; x < n; x++ {
			trans[gi][x] = (x + g) % n
		}
	}
	return &group{
		name: "Z120", order: n, gens: len(gens), trans: trans,
		label: func(i int) string { return fmt.Sprintf("%d", i) },
	}
}

// buildSn / buildZn: smaller instances for pipeline shakeout and for the
// solvable-vs-nonsolvable pair at matched order (S3 order 6 is solvable;
// A5/S5 are not). Kept so the witness can be run cheaply end to end.
func buildSmallSym(k int) *group {
	var elems [][]int
	var rec func(cur []int, used []bool)
	rec = func(cur []int, used []bool) {
		if len(cur) == k {
			c := append([]int(nil), cur...)
			elems = append(elems, c)
			return
		}
		for v := 0; v < k; v++ {
			if !used[v] {
				used[v] = true
				rec(append(cur, v), used)
				used[v] = false
			}
		}
	}
	rec(nil, make([]bool, k))
	key := func(p []int) string { return fmt.Sprint(p) }
	idx := map[string]int{}
	for i, p := range elems {
		idx[key(p)] = i
	}
	tr := make([]int, k)
	for i := range tr {
		tr[i] = i
	}
	tr[0], tr[1] = 1, 0
	cy := make([]int, k)
	for i := range cy {
		cy[i] = (i + 1) % k
	}
	gens := [][]int{tr, cy}
	trans := make([][]int, len(gens))
	for gi, g := range gens {
		trans[gi] = make([]int, len(elems))
		for pi, p := range elems {
			r := make([]int, k)
			for i := 0; i < k; i++ {
				r[i] = g[p[i]]
			}
			trans[gi][pi] = idx[key(r)]
		}
	}
	return &group{name: fmt.Sprintf("S%d", k), order: len(elems), gens: 2, trans: trans,
		label: func(i int) string { return fmt.Sprint(elems[i]) }}
}

func buildCyclic(n int, gens []int) *group {
	trans := make([][]int, len(gens))
	for gi, g := range gens {
		trans[gi] = make([]int, n)
		for x := 0; x < n; x++ {
			trans[gi][x] = (x + g) % n
		}
	}
	return &group{name: fmt.Sprintf("Z%d", n), order: n, gens: len(gens), trans: trans,
		label: func(i int) string { return fmt.Sprint(i) }}
}

func lookupGroup(name string) *group {
	switch name {
	case "s5", "S5":
		return buildS5()
	case "z120", "Z120":
		return buildZ120()
	case "s3", "S3":
		return buildSmallSym(3)
	case "z6", "Z6":
		return buildCyclic(6, []int{1, 5})
	case "s4", "S4":
		return buildSmallSym(4)
	case "z24", "Z24":
		return buildCyclic(24, []int{1, 7})
	}
	return nil
}

// identity element index: S5 enumeration puts [0 1 2 3 4] first; Z120 identity is 0.
const identityIdx = 0

// wordProduct is the sequential oracle for the ALGEBRA (independent of the SNN).
func wordProduct(G *group, word []int) int {
	x := identityIdx
	for _, g := range word {
		x = G.trans[g][x]
	}
	return x
}

// ---------------------------------------------------------------------------
// Network construction
// ---------------------------------------------------------------------------

type wordNet struct {
	G        *group
	word     []int
	sBase    int // state neuron ids: sBase + sigma
	cBase    int // conjunction ids:  cBase + pi*gens + g
	inBase   int // input line ids:   inBase + g
	fl, ih   int
	tFinal   int // tick at which the answer state neuron fires
	nNeurons int
	nSyn     int
}

func buildWord(G *group, word []int, P int) (*Network, []CMEvent, int, *wordNet) {
	n := len(word)
	net := NewNetwork(P)

	sBase := 0
	cBase := sBase + G.order
	inBase := cBase + G.order*G.gens
	fl := inBase + G.gens
	ih := fl + 1

	w := &wordNet{G: G, word: word, sBase: sBase, cBase: cBase, inBase: inBase,
		fl: fl, ih: ih, tFinal: 2 * n}

	proc := 0
	next := func() int { proc++; return 1 + (proc % P) }

	// state layer, theta = 2
	for sigma := 0; sigma < G.order; sigma++ {
		net.Add(sBase+sigma, 2, next())
	}
	// conjunction layer, theta = 2
	for pi := 0; pi < G.order; pi++ {
		for g := 0; g < G.gens; g++ {
			net.Add(cBase+pi*G.gens+g, 2, next())
		}
	}
	// input lines and clocks, theta = 1
	for g := 0; g < G.gens; g++ {
		net.Add(inBase+g, 1, 1)
	}
	net.Add(fl, 1, 1)
	// IH deliberately lives on the LAST processor so its inhibition is remote
	// relative to most state neurons: this is the straggler pressure point.
	net.Add(ih, 1, P)

	// state -> conjunction (subthreshold, +1)
	for pi := 0; pi < G.order; pi++ {
		for g := 0; g < G.gens; g++ {
			net.Connect(sBase+pi, cBase+pi*G.gens+g, +1, 1)
		}
	}
	// input -> conjunction (subthreshold, +1)
	for g := 0; g < G.gens; g++ {
		for pi := 0; pi < G.order; pi++ {
			net.Connect(inBase+g, cBase+pi*G.gens+g, +1, 1)
		}
	}
	// conjunction -> next state (suprathreshold, +2)
	for pi := 0; pi < G.order; pi++ {
		for g := 0; g < G.gens; g++ {
			net.Connect(cBase+pi*G.gens+g, sBase+G.trans[g][pi], +2, 1)
		}
	}
	// flush clock -> whole conjunction layer (+2 == theta, forces reset)
	for pi := 0; pi < G.order; pi++ {
		for g := 0; g < G.gens; g++ {
			net.Connect(fl, cBase+pi*G.gens+g, +2, 1)
		}
	}
	// cancel clock -> whole state layer, exactly -2*gens (the flush echo)
	for sigma := 0; sigma < G.order; sigma++ {
		net.Connect(ih, sBase+sigma, -2*G.gens, 1)
	}

	// stimuli
	stim := []CMEvent{{Nj: sBase + identityIdx, Ni: -1, St: 0, W: +2}}
	for k, g := range word {
		stim = append(stim, CMEvent{Nj: inBase + g, Ni: -1, St: 2 * k, W: +1})
		stim = append(stim, CMEvent{Nj: fl, Ni: -1, St: 2*k + 1, W: +1})
		stim = append(stim, CMEvent{Nj: ih, Ni: -1, St: 2*k + 2, W: +1})
	}

	w.nNeurons = len(net.Neurons)
	for _, nn := range net.Neurons {
		w.nSyn += len(nn.Out)
	}
	return net, stim, 2*n + 1, w
}

// readAnswer extracts the state neuron that fired at the final tick.
// Returns (element index, ok). ok=false means the tracker was destroyed:
// no state fired, or more than one did (one-hot invariant broken).
func (w *wordNet) readAnswer(train [][2]int) (int, bool) {
	found, count := -1, 0
	for _, p := range train {
		if p[0] != w.tFinal {
			continue
		}
		id := p[1]
		if id >= w.sBase && id < w.sBase+w.G.order {
			found = id - w.sBase
			count++
		}
	}
	return found, count == 1
}

// oneHotOK checks the invariant that at every even tick exactly one state
// neuron fires. This is a stronger and more diagnostic check than the final
// answer alone.
func (w *wordNet) oneHotOK(train [][2]int) bool {
	n := len(w.word)
	for k := 0; k <= n; k++ {
		cnt := 0
		for _, p := range train {
			if p[0] == 2*k && p[1] >= w.sBase && p[1] < w.sBase+w.G.order {
				cnt++
			}
		}
		if cnt != 1 {
			return false
		}
	}
	return true
}

func randomWord(G *group, n int, seed int64) []int {
	r := rand.New(rand.NewSource(seed))
	w := make([]int, n)
	for i := range w {
		w[i] = r.Intn(G.gens)
	}
	return w
}

// ---------------------------------------------------------------------------
// Witness driver
// ---------------------------------------------------------------------------

type witnessResult struct {
	Group        string
	N            int
	P            int
	Guard        string
	Neurons      int
	Synapses     int
	Depth        int
	EncodingOK   bool // sequential oracle reproduces the true group product
	Runs         int
	TrainMism    int // distributed spike train != sequential ground truth
	AnswerWrong  int // distributed final state != true group product
	OneHotBroken int
	Unsound      int
	Truncated    int
	Extra        int
	Reordered    int
}

func runWordWitness(groupName string, n, P, nWords, reps int, cfg Config) witnessResult {
	G := lookupGroup(groupName)
	res := witnessResult{Group: G.name, N: n, P: P, Guard: cfg.Guard, Depth: 2 * n, EncodingOK: true}

	for wi := 0; wi < nWords; wi++ {
		word := randomWord(G, n, int64(1000+wi))
		net, stim, tMax, w := buildWord(G, word, P)
		res.Neurons, res.Synapses = w.nNeurons, w.nSyn

		cfg2 := cfg
		cfg2.TMax = tMax

		truth := sequentialEDS(net, stim, tMax)
		want := wordProduct(G, word)
		got, ok := w.readAnswer(truth)
		if !ok || got != want || !w.oneHotOK(truth) {
			res.EncodingOK = false
		}

		for r := 0; r < reps; r++ {
			train, bad, _ := runOnce(net, stim, cfg2)
			if !equalPairs(train, truth) {
				res.TrainMism++
				if len(train) < len(truth) {
					res.Truncated++
				} else if len(train) > len(truth) {
					res.Extra++
				} else {
					res.Reordered++
				}
			}
			if bad {
				res.Unsound++
			}
			a, aok := w.readAnswer(train)
			if !aok || a != want {
				res.AnswerWrong++
			}
			if !w.oneHotOK(train) {
				res.OneHotBroken++
			}
			res.Runs++
		}
	}
	return res
}

func (r witnessResult) print() {
	fmt.Printf("group=%-5s n=%-3d P=%d guard=%s | neurons=%d synapses=%d depth=%d | encoding=%v\n",
		r.Group, r.N, r.P, r.Guard, r.Neurons, r.Synapses, r.Depth, r.EncodingOK)
	fmt.Printf("   runs=%d  train-mismatch=%d (short=%d extra=%d same-len=%d)  answer-wrong=%d  one-hot-broken=%d  flagged-unsound=%d\n",
		r.Runs, r.TrainMism, r.Truncated, r.Extra, r.Reordered, r.AnswerWrong, r.OneHotBroken, r.Unsound)
}
