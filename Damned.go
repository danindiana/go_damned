// damned.go — a living slice of the DAMNED simulator.
//
// DAMNED = Distributed And Multithreaded Neural Event Driven simulation
// (Mouraud, Puzenat & Paugam-Moisy, 2006/2009). This is a faithful, runnable
// reconstruction of the distributed event-driven spiking-neural-network engine,
// written the way DAMNED wants to be written: one goroutine per processor, plus
// controller goroutines, communicating over channels.
//
// The mapping from the paper to Go is almost one-to-one:
//
//     paper                         Go
//     ---------------------------   --------------------------------------
//     processor Pr_p                a goroutine running processorLoop()
//     CMC / CPC controller threads  the select loop inside that goroutine
//     EC thread (one per neuron)    a goroutine spawned per computation
//     MPI message between Pr_p,Pr_m a value sent on an inbox channel
//     per-channel FIFO (MPI)        Go channels are FIFO  <-- load-bearing
//     Pr0 environment processor     the coordinator goroutine
//
// The one subtlety this whole program turns on: an event-driven SNN lets an
// inhibitory spike with a short delay cancel a previously forecast spike (the
// "delayed firing problem", Fig. 1 of the paper). A distributed simulator must
// therefore never *commit* an emission until it is certain no earlier-arriving
// spike can still cancel it. DAMNED 2006 got the emission guard subtly wrong;
// DAMNED 2009 fixed it by adopting the Chandy-Misra-Bryant discipline: emit
// only once every peer's lower-bound-on-future-emissions (an LBTS) has passed
// the event's stamp, and advertise your own bound via null messages.
//
// This program implements BOTH guards. Run with -guard=2009 (default, correct)
// or -guard=2006 (the original, which can commit spikes that never happen).
// A sequential ground-truth simulator checks the distributed spike train.
//
//   go run damned.go                 # correct engine, cancellation scenario
//   go run damned.go -guard=2006     # watch exactness break
//   go run damned.go -scenario=ring  # a busier recurrent network
//
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime/trace"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Network
// ---------------------------------------------------------------------------

type Synapse struct {
	Post int // target neuron id
	W    int // integer weight (negative = inhibitory)
	D    int // integer delay >= 1  (this is the per-synapse lookahead)
}

type Neuron struct {
	ID    int
	Theta int       // firing threshold
	Out   []Synapse // postsynaptic connections
	Proc  int       // processor this neuron is mapped to (1..P)
}

type Network struct {
	Neurons map[int]*Neuron
	P       int // number of computing processors
}

func NewNetwork(p int) *Network { return &Network{Neurons: map[int]*Neuron{}, P: p} }

func (n *Network) Add(id, theta, proc int) {
	n.Neurons[id] = &Neuron{ID: id, Theta: theta, Proc: proc}
}
func (n *Network) Connect(pre, post, w, d int) {
	if d < 1 {
		panic("delay must be >= 1")
	}
	nn := n.Neurons[pre]
	nn.Out = append(nn.Out, Synapse{Post: post, W: w, D: d})
}

// dMin is the global minimal synaptic delay: the network-wide lookahead.
func (n *Network) dMin() int {
	m := 1 << 30
	for _, nn := range n.Neurons {
		for _, s := range nn.Out {
			if s.D < m {
				m = s.D
			}
		}
	}
	if m == 1<<30 {
		return 1
	}
	return m
}

// ---------------------------------------------------------------------------
// Events and messages
// ---------------------------------------------------------------------------

// A CM event: "source Ni spiked; it impacts target Nj at time St."
// St is the IMPACT time (emission stamp + synaptic delay) — the quantity
// causality actually depends on.
type CMEvent struct {
	Nj, Ni int
	St     int
	W      int // impact weight, carried so the target need not know the synapse
}

// A CP event: "neuron Ni is forecast to fire at St." Crt marks it certified.
type CPEvent struct {
	Ni  int
	St  int
	Crt bool
}

// A message on the wire between processors. Besides any events, every message
// carries the sender's advertised lower bound on all its future emissions
// (Bnd) — the 2009 null-message content. Src identifies the sender so the
// receiver files the bound in the right slot.
type Message struct {
	Src  int
	Evs  []CMEvent
	Bnd  int  // sender's LBTS: no future emission from Src will have stamp < Bnd
	Fin  bool // sender has permanently finished (used only for shutdown)
}

const INF = 1 << 30

// ecResult is what an EC thread returns to its processor's result collector.
type ecResult struct {
	fired bool
	cp    CPEvent
	ev    CMEvent
}

// ---------------------------------------------------------------------------
// Ground-truth sequential event-driven simulator
// ---------------------------------------------------------------------------
//
// One global queue of impacts ordered by (time, inhibition-before-excitation).
// This is the reference the distributed run must reproduce exactly.

type impact struct {
	t   int
	exc int // 0 for inhibitory (sorts first), 1 for excitatory
	seq int
	nid int
	w   int
}

func sequentialEDS(net *Network, stim []CMEvent, tMax int) [][2]int {
	q := make([]impact, 0, len(stim))
	seq := 0
	for _, e := range stim {
		ex := 1
		if e.W < 0 {
			ex = 0
		}
		q = append(q, impact{t: e.St, exc: ex, seq: seq, nid: e.Nj, w: e.W})
		seq++
	}
	less := func(a, b impact) bool {
		if a.t != b.t {
			return a.t < b.t
		}
		if a.exc != b.exc {
			return a.exc < b.exc
		}
		return a.seq < b.seq
	}
	pot := map[int]int{}
	firedAt := map[int]int{}
	var fired [][2]int
	for len(q) > 0 {
		// pop min
		mi := 0
		for i := 1; i < len(q); i++ {
			if less(q[i], q[mi]) {
				mi = i
			}
		}
		e := q[mi]
		q = append(q[:mi], q[mi+1:]...)
		if e.t > tMax {
			continue
		}
		pot[e.nid] += e.w
		nn := net.Neurons[e.nid]
		if pot[e.nid] >= nn.Theta && firedAt[e.nid] != e.t+1 {
			fired = append(fired, [2]int{e.t, e.nid})
			firedAt[e.nid] = e.t + 1
			pot[e.nid] = 0
			for _, s := range nn.Out {
				ex := 1
				if s.W < 0 {
					ex = 0
				}
				q = append(q, impact{t: e.t + s.D, exc: ex, seq: seq, nid: s.Post, w: s.W})
				seq++
			}
		}
	}
	sortPairs(fired)
	return fired
}

// ---------------------------------------------------------------------------
// The distributed engine
// ---------------------------------------------------------------------------

type Config struct {
	Guard   string        // "2009" (correct) or "2006" (original, unsound)
	TMax    int
	Latency time.Duration // simulated inter-processor message latency
	Verbose bool          // print each commit, and flag stragglers as they happen
	Trace   bool          // collect a machine-readable event trace for visualization
}

// TraceEvent is one entry in the machine-readable trace (JSON-emitted).
type TraceEvent struct {
	Kind      string `json:"kind"`   // "commit" | "send" | "recv" | "compute" | "spike"
	TNanos    int64  `json:"tns"`    // wall-clock nanoseconds since run start
	Proc      int    `json:"proc"`   // processor raising the event (0 = Pr0)
	Dst       int    `json:"dst"`    // destination processor (for send/recv)
	Neuron    int    `json:"neuron"`
	Stamp     int    `json:"stamp"`  // logical spike stamp
	Status    string `json:"status"` // "ok" | "straggler"
	WitnessNj int    `json:"wnj"`
	WitnessSt int    `json:"wst"`
	WitnessW  int    `json:"ww"`
}

// A priority queue of CM events (min stamp; inhibition before excitation).
type cmQueue struct{ items []CMEvent }

func (q *cmQueue) push(e CMEvent) { q.items = append(q.items, e) }
func (q *cmQueue) empty() bool    { return len(q.items) == 0 }
func (q *cmQueue) pop() CMEvent {
	mi := 0
	for i := 1; i < len(q.items); i++ {
		a, b := q.items[i], q.items[mi]
		aex, bex := 1, 1
		if a.W < 0 {
			aex = 0
		}
		if b.W < 0 {
			bex = 0
		}
		if a.St < b.St || (a.St == b.St && aex < bex) {
			mi = i
		}
	}
	e := q.items[mi]
	q.items = append(q.items[:mi], q.items[mi+1:]...)
	return e
}
func (q *cmQueue) minStamp() int {
	m := INF
	for _, e := range q.items {
		if e.St < m {
			m = e.St
		}
	}
	return m
}

type engine struct {
	net  *Network
	cfg  Config
	dmin int

	inbox []chan Message // inbox[p], p in 1..P  (FIFO channels)
	wire  []chan Message // wire[p]: ordered pre-inbox stage that applies latency
	toEnv chan Message   // processors -> Pr0 (output spikes + bounds)

	committed []([2]int) // the distributed spike train (guarded by mu)
	events    []TraceEvent // machine-readable trace (guarded by mu)
	t0        time.Time    // run start, for relative timestamps
	mu        sync.Mutex

	bad int32 // set !=0 if any emission was committed unsoundly

	inFlight int64 // messages sent but not yet processed (termination detection)
	pending  int64 // events queued/among all processors but not yet emitted-or-dropped

	liveMu sync.Mutex // guards live: every impact created but not yet computed,
	live   []CMEvent  // wherever it is (queue, running, or in flight on a wire)

	wg   sync.WaitGroup
	done chan struct{}
}

// PendingWouldCancel reports whether, at the moment neuron ni is about to be
// committed as firing at stamp st, there still exists anywhere in the system an
// unaccounted-for event that impacts ni at or before st. If so, committing is
// unsound: a real (sequential) run might have had that event cancel the spike.
// emit records a trace event (only when tracing is enabled).
func (e *engine) emit(ev TraceEvent) {
	if !e.cfg.Trace {
		return
	}
	ev.TNanos = time.Since(e.t0).Nanoseconds()
	e.mu.Lock()
	e.events = append(e.events, ev)
	e.mu.Unlock()
}

// This is the oracle the paper never had; we use it only to *detect* violations.
// It returns the cancelling event it found (and true), if any, so a verbose
// caller can name the straggler that makes this commit unsound.
func (e *engine) markIfUnsound(procState []*procLocal, ni, st int) (CMEvent, bool) {
	var witness CMEvent
	found := false
	// in-flight impacts (on a wire, in an inbox, or freshly queued) that could
	// still land on ni at or before st — the ones a queue scan alone misses.
	e.liveMu.Lock()
	for _, ev := range e.live {
		if ev.Nj == ni && ev.St <= st {
			atomic.StoreInt32(&e.bad, 1)
			witness, found = ev, true
		}
	}
	e.liveMu.Unlock()
	for _, pl := range procState {
		if pl == nil {
			continue // slot 0 is Pr0, which owns no neurons
		}
		pl.mu.Lock()
		for _, ev := range pl.cm.items {
			if ev.Nj == ni && ev.St <= st {
				atomic.StoreInt32(&e.bad, 1)
				witness, found = ev, true
			}
		}
		for _, ev := range pl.running {
			if ev.Nj == ni && ev.St <= st {
				atomic.StoreInt32(&e.bad, 1)
				witness, found = ev, true
			}
		}
		pl.mu.Unlock()
	}
	return witness, found
}

// send delivers a message to a processor over its ordered "wire". Every message
// bound for dst — event, null/bound, or stimulus — goes through the SAME wire,
// so per-channel FIFO is preserved even when latency is applied. This is the
// non-negotiable requirement the correctness argument turns on: a bound update
// must never overtake an event it was meant to guard. (Mixing an immediate
// bound-send with a delayed event-send is exactly how that invariant breaks.)
func (e *engine) send(dst int, m Message) {
	if e.cfg.Trace && len(m.Evs) > 0 {
		for _, ev := range m.Evs {
			e.emit(TraceEvent{Kind: "send", Proc: m.Src, Dst: dst,
				Neuron: ev.Nj, Stamp: ev.St, WitnessW: ev.W})
		}
	}
	e.wire[dst] <- m
}

// wireLoop drains one destination's wire in order, applies latency, and pushes
// to the inbox. A single goroutine per destination guarantees that messages
// leave in the order they were enqueued: FIFO with delay.
func (e *engine) wireLoop(dst int) {
	defer e.wg.Done()
	for {
		select {
		case m := <-e.wire[dst]:
			if e.cfg.Latency > 0 {
				time.Sleep(e.cfg.Latency)
			}
			if e.cfg.Trace && len(m.Evs) > 0 {
				for _, ev := range m.Evs {
					e.emit(TraceEvent{Kind: "recv", Proc: m.Src, Dst: dst,
						Neuron: ev.Nj, Stamp: ev.St, WitnessW: ev.W})
				}
			}
			e.inbox[dst] <- m
		case <-e.done:
			return
		}
	}
}

func (e *engine) liveAdd(ev CMEvent) {
	e.liveMu.Lock()
	e.live = append(e.live, ev)
	e.liveMu.Unlock()
}
func (e *engine) liveDone(ev CMEvent) {
	e.liveMu.Lock()
	for i, x := range e.live {
		if x == ev {
			e.live = append(e.live[:i], e.live[i+1:]...)
			break
		}
	}
	e.liveMu.Unlock()
}

// procLocal is the mutable state of one processor.
type procLocal struct {
	id      int
	cm      cmQueue           // incoming spikes to compute
	cp      []CPEvent         // forecast spikes to emit
	running []CMEvent         // events currently "in an EC thread"
	pot     map[int]int       // membrane potentials of local neurons
	firedAt map[int]int       // simple refractory
	clk     map[int]int       // known LBTS of each peer (0..P), INF = finished
	et      int               // last committed emission stamp (for reporting)
	mu      sync.Mutex
}

// myBound computes this processor's LBTS: the least stamp it could still emit.
// It is derived from its own pending work AND every peer bound it knows, plus
// the network lookahead dmin — exactly CMB's rule
//   output_bound = min(inputs) + lookahead.
// If it has nothing pending and every peer says INF, it too is INF.
func (pl *procLocal) myBound(dmin int) int {
	m := INF
	for _, e := range pl.cm.items {
		if e.St < m {
			m = e.St
		}
	}
	for _, e := range pl.running {
		if e.St < m {
			m = e.St
		}
	}
	for _, c := range pl.cp {
		if c.St < m {
			m = c.St
		}
	}
	for _, b := range pl.clk {
		if b < m {
			m = b
		}
	}
	if m >= INF {
		return INF
	}
	return m + dmin
}

// localClear reports whether no unprocessed local event can still land on
// neuron n at or before stamp s.
func (pl *procLocal) localClear(n, s int) bool {
	for _, e := range pl.cm.items {
		if e.Nj == n && e.St <= s {
			return false
		}
	}
	for _, e := range pl.running {
		if e.Nj == n && e.St <= s {
			return false
		}
	}
	return true
}

// emitAuthorized applies the guard under test to the top CP event.
func (e *engine) emitAuthorized(pl *procLocal, c CPEvent) bool {
	if e.cfg.Guard == "2006" {
		// The original disjunction (section 4). Three of the four branches
		// silently assume no straggler exists; the "peer looks idle" disjunct
		// (clk<=0) is the hole.
		if c.St == pl.et {
			return true
		}
		if c.Crt {
			return true
		}
		// "st <= pt": approximated here by "local processing has passed st",
		// i.e. nothing local pending at or before st.
		if pl.localClear(c.Ni, c.St) {
			// still admit even if peers might have stragglers — the bug
			return true
		}
		return false
	}
	// 2009 discipline: local clear AND every peer's channel clock strictly past st.
	if !pl.localClear(c.Ni, c.St) {
		return false
	}
	for m, b := range pl.clk {
		_ = m
		if !(c.St < b) {
			return false
		}
	}
	return true
}

func main() {
	guard := flag.String("guard", "2009", "emission guard: 2009 (correct) or 2006 (unsound)")
	scenario := flag.String("scenario", "cancel", "scenario: cancel | ring")
	reps := flag.Int("reps", 200, "repetitions (concurrency is nondeterministic)")
	lat := flag.Duration("latency", 0, "simulated inter-processor message latency, e.g. 2ms")
	verbose := flag.Bool("verbose", false, "print each commit as it happens; forces reps=1")
	traceFile := flag.String("trace", "", "write a JSON event trace of one run to this file")
	runTrace := flag.String("runtrace", "", "write a Go runtime execution trace (open with: go tool trace FILE)")
	flag.Parse()

	var net *Network
	var stim []CMEvent
	tMax := 12
	switch *scenario {
	case "cancel":
		net, stim = buildCancellation()
	case "ring":
		net, stim, tMax = buildRing()
	default:
		fmt.Println("unknown scenario")
		return
	}

	truth := sequentialEDS(net, stim, tMax)
	fmt.Printf("scenario=%s  guard=%s  P=%d  dmin=%d  latency=%v\n", *scenario, *guard, net.P, net.dMin(), *lat)
	fmt.Printf("ground-truth spikes (%d): %v\n", len(truth), truth)

	if *runTrace != "" {
		f, err := os.Create(*runTrace)
		if err != nil {
			fmt.Println("runtrace create error:", err)
			return
		}
		if err := trace.Start(f); err != nil {
			fmt.Println("trace start error:", err)
			return
		}
		// a few reps so the timeline shows repeated waves of goroutine activity
		n := *reps
		if n > 20 {
			n = 20
		}
		for r := 0; r < n; r++ {
			runOnce(net, stim, Config{Guard: *guard, TMax: tMax, Latency: *lat})
		}
		trace.Stop()
		f.Close()
		fmt.Printf("wrote runtime trace to %s\n", *runTrace)
		fmt.Printf("open it with:  go tool trace %s\n", *runTrace)
		return
	}

	if *verbose {
		*reps = 1
		fmt.Println("--- verbose trace of a single run ---")
	}
	if *traceFile != "" {
		// one traced run, written as JSON for the visualizer
		_, _, evs := runOnce(net, stim, Config{Guard: *guard, TMax: tMax, Latency: *lat, Trace: true})
		blob, _ := json.MarshalIndent(map[string]any{
			"scenario": *scenario, "guard": *guard, "P": net.P,
			"latency": lat.String(), "truth": truth, "events": evs,
		}, "", "  ")
		if err := os.WriteFile(*traceFile, blob, 0644); err != nil {
			fmt.Println("trace write error:", err)
		} else {
			fmt.Printf("wrote trace of %d events to %s\n", len(evs), *traceFile)
		}
		return
	}

	mism, badCount := 0, 0
	var sample [][2]int
	for r := 0; r < *reps; r++ {
		got, bad, _ := runOnce(net, stim, Config{Guard: *guard, TMax: tMax, Latency: *lat, Verbose: *verbose})
		if !equalPairs(got, truth) {
			mism++
			if sample == nil {
				sample = got
			}
		}
		if bad {
			badCount++
		}
	}
	fmt.Printf("distributed engine over %d runs:\n", *reps)
	fmt.Printf("  spike-train mismatches vs ground truth : %d/%d\n", mism, *reps)
	fmt.Printf("  runs flagged unsound (straggler commit): %d/%d\n", badCount, *reps)
	if sample != nil {
		fmt.Printf("  example divergent train: %v\n", sample)
	}
	if mism == 0 && badCount == 0 {
		fmt.Println("  => every interleaving matched the sequential truth. Sound.")
	} else {
		fmt.Println("  => exactness violated under concurrency. Unsound guard.")
	}
}

// runOnce spins up the whole organism: P processor goroutines + Pr0, lets it
// breathe until quiescent, and returns the committed spike train.
func runOnce(net *Network, stim []CMEvent, cfg Config) ([][2]int, bool, []TraceEvent) {
	e := &engine{
		net:  net,
		cfg:  cfg,
		dmin: net.dMin(),
		done: make(chan struct{}),
		t0:   time.Now(),
	}
	P := net.P
	e.inbox = make([]chan Message, P+1)
	e.wire = make([]chan Message, P+1)
	for p := 1; p <= P; p++ {
		e.inbox[p] = make(chan Message, 4096)
		e.wire[p] = make(chan Message, 4096)
	}
	e.toEnv = make(chan Message, 4096)
	// one forwarder goroutine per destination: FIFO + latency
	for p := 1; p <= P; p++ {
		e.wg.Add(1)
		go e.wireLoop(p)
	}

	// shared local states so the oracle can inspect all processors
	states := make([]*procLocal, P+1)
	for p := 1; p <= P; p++ {
		states[p] = newProcLocal(p, P)
	}

	// launch processors
	for p := 1; p <= P; p++ {
		e.wg.Add(1)
		go e.processorLoop(states[p], states)
	}

	// launch Pr0 (environment): injects stimuli, broadcasts its bound, detects
	// global quiescence, and shuts the organism down.
	e.wg.Add(1)
	go e.environmentLoop(stim, states)

	e.wg.Wait()

	e.mu.Lock()
	out := make([][2]int, len(e.committed))
	copy(out, e.committed)
	evs := make([]TraceEvent, len(e.events))
	copy(evs, e.events)
	e.mu.Unlock()
	sortPairs(out)
	return out, atomic.LoadInt32(&e.bad) != 0, evs
}

func newProcLocal(id, P int) *procLocal {
	pl := &procLocal{
		id:      id,
		pot:     map[int]int{},
		firedAt: map[int]int{},
		clk:     map[int]int{},
	}
	// initially every peer (including Pr0=0) may emit as early as 0
	for m := 0; m <= P; m++ {
		if m != id {
			pl.clk[m] = 0
		}
	}
	return pl
}

// ---------------------------------------------------------------------------
// Pr0 — the environment processor. Not a farmer: it injects stimuli, advertises
// the injection bound, and performs termination detection. Its "bound" is the
// smallest stamp it might still inject; once stimuli are exhausted it is INF.
// ---------------------------------------------------------------------------

func (e *engine) environmentLoop(stim []CMEvent, states []*procLocal) {
	defer e.wg.Done()
	P := e.net.P

	// group stimuli by injection time (their impact stamp), inject in order
	byTime := map[int][]CMEvent{}
	maxT := 0
	for _, s := range stim {
		byTime[s.St] = append(byTime[s.St], s)
		if s.St > maxT {
			maxT = s.St
		}
	}

	// inject everything up front but stamp-ordered; each carries Pr0's residual
	// bound so processors know how early Pr0 might still speak.
	times := make([]int, 0, len(byTime))
	for t := range byTime {
		times = append(times, t)
	}
	sort.Ints(times)
	for _, t := range times {
		bnd := t // Pr0 will not inject anything earlier than t from now on
		for _, ev := range byTime[t] {
			atomic.AddInt64(&e.pending, 1)
			atomic.AddInt64(&e.inFlight, 1)
			e.liveAdd(ev)
			e.send(e.net.Neurons[ev.Nj].Proc, Message{Src: 0, Evs: []CMEvent{ev}, Bnd: bnd})
		}
	}
	// after the last stimulus, Pr0's bound becomes INF: it will never speak
	// again. Broadcast that so idle processors can raise their own bounds.
	for p := 1; p <= P; p++ {
		e.send(p, Message{Src: 0, Bnd: INF})
	}

	// termination detection: drain output spikes and watch the global counters.
	// When no messages are in flight and nothing is pending anywhere, the
	// organism is quiescent and we can stop. This quiescence check stands in
	// for the paper's distributed clock + Pr0 time-out; it is a convenience of
	// having all goroutines in one address space, not an extra synchronizer the
	// processors rely on.
	quietStreak := 0
	for {
		select {
		case m := <-e.toEnv:
			_ = m // output spikes reach the environment here; we just count them
			atomic.AddInt64(&e.inFlight, -1)
		case <-time.After(200 * time.Microsecond):
		}
		inflight := atomic.LoadInt64(&e.inFlight)
		pending := atomic.LoadInt64(&e.pending)
		allIdle := true
		for p := 1; p <= P; p++ {
			if len(e.inbox[p]) > 0 {
				allIdle = false
				break
			}
		}
		if inflight == 0 && pending == 0 && allIdle {
			quietStreak++
		} else {
			quietStreak = 0
		}
		if quietStreak > 3 {
			break
		}
	}
	close(e.done)
}

// ---------------------------------------------------------------------------
// A processor: CMC (reception, emission) + CPC (computation) fused into one
// select loop, spawning an EC "thread" per computation. No barrier anywhere.
// ---------------------------------------------------------------------------

func (e *engine) processorLoop(pl *procLocal, states []*procLocal) {
	defer e.wg.Done()
	// name this goroutine's region so it shows up legibly in `go tool trace`
	ctx, task := trace.NewTask(context.Background(), fmt.Sprintf("Pr%d", pl.id))
	defer task.End()
	_ = ctx
	P := e.net.P

	// results from EC threads come back here
	results := make(chan ecResult, 256)
	var ecWG sync.WaitGroup

	tick := time.NewTicker(150 * time.Microsecond)
	defer tick.Stop()

	lastBound := -1
	broadcastBound := func() {
		// null message: advertise our current LBTS to every peer, but only when
		// it changes, and always through the ordered wire so it cannot overtake
		// an event we sent earlier.
		b := pl.myBound(e.dmin)
		if b == lastBound {
			return
		}
		lastBound = b
		for m := 1; m <= P; m++ {
			if m == pl.id {
				continue
			}
			e.send(m, Message{Src: pl.id, Bnd: b})
		}
	}

	for {
		select {
		case <-e.done:
			ecWG.Wait()
			return

		case msg := <-e.inbox[pl.id]:
			// CMC step 1: reception + clock/bound merge (FIFO channel => the
			// latest bound from a source is the freshest; overwrite).
			pl.mu.Lock()
			pl.clk[msg.Src] = msg.Bnd
			for _, ev := range msg.Evs {
				pl.cm.push(ev)
			}
			pl.mu.Unlock()
			if len(msg.Evs) > 0 {
				atomic.AddInt64(&e.inFlight, -1)
			}
			e.tryComputeAndEmit(pl, states, &ecWG, results)

		case r := <-results:
			// CPC step 2: result collector.
			pl.mu.Lock()
			// remove from running
			for i, ev := range pl.running {
				if ev == r.ev {
					pl.running = append(pl.running[:i], pl.running[i+1:]...)
					break
				}
			}
			if r.fired {
				pl.cp = append(pl.cp, r.cp)
			}
			pl.mu.Unlock()
			atomic.AddInt64(&e.pending, -1) // this CM event is now resolved
			e.tryComputeAndEmit(pl, states, &ecWG, results)

		case <-tick.C:
			// periodically advertise our bound and retry blocked work, so that
			// bounds propagate and nobody is stuck on a stale clock.
			broadcastBound()
			e.tryComputeAndEmit(pl, states, &ecWG, results)
		}
	}
}

// tryComputeAndEmit runs the CPC computation-starter and the CMC emission-
// control as far as they will go given current authorizations.
func (e *engine) tryComputeAndEmit(pl *procLocal, states []*procLocal, ecWG *sync.WaitGroup, results chan ecResult) {
	// --- CPC: start computations that are authorized ---
	for {
		pl.mu.Lock()
		if pl.cm.empty() {
			pl.mu.Unlock()
			break
		}
		// computation authorization: for 2009, the top event's stamp must be
		// strictly below every peer bound (nothing earlier can still arrive);
		// for 2006, we approximate the paper's pt/clk disjunction and let it
		// proceed more eagerly (which is exactly what lets stragglers slip).
		top := pl.cm.minStamp()
		ok := true
		if e.cfg.Guard == "2009" {
			for _, b := range pl.clk {
				if !(top < b) {
					ok = false
					break
				}
			}
		}
		if !ok {
			pl.mu.Unlock()
			break
		}
		ev := pl.cm.pop()
		// one computation at a time per neuron
		busy := false
		for _, r := range pl.running {
			if r.Nj == ev.Nj {
				busy = true
			}
		}
		if busy {
			pl.cm.push(ev)
			pl.mu.Unlock()
			break
		}
		pl.running = append(pl.running, ev)
		pl.mu.Unlock()

		// spawn the EC thread
		ecWG.Add(1)
		go func(ev CMEvent) {
			defer ecWG.Done()
			trace.WithRegion(context.Background(), fmt.Sprintf("EC-N%d@%d", ev.Nj, ev.St), func() {
				fired, cp := e.computeNeuron(pl, ev)
				results <- ecResult{fired: fired, cp: cp, ev: ev}
			})
		}(ev)
	}

	// --- CMC: emit authorized CP events ---
	for {
		pl.mu.Lock()
		if len(pl.cp) == 0 {
			pl.mu.Unlock()
			break
		}
		// pick the least-stamp CP event
		mi := 0
		for i := 1; i < len(pl.cp); i++ {
			if pl.cp[i].St < pl.cp[mi].St {
				mi = i
			}
		}
		c := pl.cp[mi]
		if !e.emitAuthorized(pl, c) {
			pl.mu.Unlock()
			break
		}
		// remove it
		pl.cp = append(pl.cp[:mi], pl.cp[mi+1:]...)
		pl.et = c.St
		pl.mu.Unlock()

		// ---- COMMIT POINT: check soundness against the whole system ----
		witness, unsound := e.markIfUnsound(states, c.Ni, c.St)
		if e.cfg.Verbose {
			if unsound {
				sign := "excitatory"
				if witness.W < 0 {
					sign = "INHIBITORY"
				}
				fmt.Printf("  [Pr%d] t=%-2d COMMIT N%d@%d  <<< STRAGGLER: %s impact on N%d@%d (from N%d, w=%+d) still unaccounted for — this spike should have been cancelled\n",
					pl.id, c.St, c.Ni, c.St, sign, witness.Nj, witness.St, witness.Ni, witness.W)
			} else {
				fmt.Printf("  [Pr%d] t=%-2d commit  N%d@%d  (sound)\n", pl.id, c.St, c.Ni, c.St)
			}
		}
		if e.cfg.Trace {
			tag := "ok"
			if unsound {
				tag = "straggler"
			}
			e.emit(TraceEvent{
				Kind: "commit", Proc: pl.id, Neuron: c.Ni, Stamp: c.St, Status: tag,
				WitnessNj: witness.Nj, WitnessSt: witness.St, WitnessW: witness.W,
			})
		}
		e.mu.Lock()
		e.committed = append(e.committed, [2]int{c.St, c.Ni})
		e.mu.Unlock()

		// fan out CM events to postsynaptic targets
		nn := e.net.Neurons[c.Ni]
		local := []CMEvent{}
		remote := map[int][]CMEvent{}
		for _, s := range nn.Out {
			st := c.St + s.D
			if st > e.cfg.TMax {
				continue
			}
			out := CMEvent{Nj: s.Post, Ni: c.Ni, St: st, W: s.W}
			tp := e.net.Neurons[s.Post].Proc
			if tp == pl.id {
				local = append(local, out)
			} else {
				remote[tp] = append(remote[tp], out)
			}
		}
		if len(local) > 0 {
			pl.mu.Lock()
			for _, o := range local {
				pl.cm.push(o)
				atomic.AddInt64(&e.pending, 1)
				e.liveAdd(o)
			}
			pl.mu.Unlock()
		}
		b := func() int { pl.mu.Lock(); defer pl.mu.Unlock(); return pl.myBound(e.dmin) }()
		for tp, evs := range remote {
			for _, o := range evs {
				atomic.AddInt64(&e.pending, 1)
				atomic.AddInt64(&e.inFlight, 1)
				e.liveAdd(o)
			}
			e.send(tp, Message{Src: pl.id, Evs: evs, Bnd: b})
		}
		// report output spike to the environment (spikes from output neurons)
		select {
		case e.toEnv <- Message{Src: pl.id, Bnd: b}:
			atomic.AddInt64(&e.inFlight, 1)
		default:
		}
	}
}

// computeNeuron is the EC thread body: integrate the impact into the neuron's
// potential and forecast a spike if threshold is crossed.
func (e *engine) computeNeuron(pl *procLocal, ev CMEvent) (bool, CPEvent) {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	e.liveDone(ev)
	e.emit(TraceEvent{Kind: "compute", Proc: pl.id, Neuron: ev.Nj, Stamp: ev.St, WitnessW: ev.W})
	nn := e.net.Neurons[ev.Nj]
	pl.pot[ev.Nj] += ev.W
	if pl.pot[ev.Nj] >= nn.Theta && pl.firedAt[ev.Nj] != ev.St+1 {
		pl.firedAt[ev.Nj] = ev.St + 1
		pl.pot[ev.Nj] = 0
		return true, CPEvent{Ni: ev.Nj, St: ev.St, Crt: false}
	}
	return false, CPEvent{}
}

// ---------------------------------------------------------------------------
// Scenarios
// ---------------------------------------------------------------------------

// buildCancellation: the delayed-firing counterexample. Postsynaptic N0
// (theta=4) gets two excitatory impacts summing to threshold at t=3, plus a
// remote inhibitory impact (source on processor 2) also at t=3 with a smaller
// delay. Correct answer: N0 never fires. A stale-clock emission would commit it.
func buildCancellation() (*Network, []CMEvent) {
	net := NewNetwork(2)
	net.Add(0, 4, 1) // postsynaptic on proc 1
	net.Add(1, 1, 1) // exc source, proc 1
	net.Add(2, 1, 1) // exc source, proc 1
	net.Add(3, 1, 2) // INHIB source, proc 2
	net.Connect(1, 0, +2, 3) // impact t=3
	net.Connect(2, 0, +2, 2) // impact t=3
	net.Connect(3, 0, -3, 1) // impact t=3, shorter delay
	stim := []CMEvent{
		{Nj: 1, Ni: -1, St: 0, W: +1},
		{Nj: 2, Ni: -1, St: 1, W: +1},
		{Nj: 3, Ni: -1, St: 2, W: +1},
	}
	return net, stim
}

// buildRing: a small recurrent excitatory ring across 3 processors, with one
// inhibitory chord, to exercise cross-processor traffic and the delayed queue.
func buildRing() (*Network, []CMEvent, int) {
	P := 3
	net := NewNetwork(P)
	n := 6
	for i := 0; i < n; i++ {
		net.Add(i, 2, 1+(i%P))
	}
	for i := 0; i < n; i++ {
		net.Connect(i, (i+1)%n, +2, 1+(i%2)) // ring, delays 1/2
	}
	net.Connect(0, 3, -3, 1) // an inhibitory chord
	stim := []CMEvent{
		{Nj: 0, Ni: -1, St: 0, W: +2},
		{Nj: 1, Ni: -1, St: 0, W: +2},
	}
	return net, stim, 10
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func sortPairs(p [][2]int) {
	sort.Slice(p, func(i, j int) bool {
		if p[i][0] != p[j][0] {
			return p[i][0] < p[j][0]
		}
		return p[i][1] < p[j][1]
	})
}

func equalPairs(a, b [][2]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
