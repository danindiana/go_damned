# Algorithm Analysis & Visual Guides

This folder contains 9 high-level visual diagrams that deconstruct the DAMNED algorithm layer by layer: system architecture, processor internals, event flow, the core problem, and the solutions (2006 vs 2009).

These diagrams are the visual complement to the Go code and formal specs. If you understand these images, you understand DAMNED.

## Diagram Series: 01–07 (Algorithm Breakdown)

A progressive decomposition of DAMNED from system level down to clock propagation.

### 01_system_architecture.png — Birds-Eye View

**What it shows:**
- **Pr0 (environment processor)**: injects stimuli, broadcasts bounds, detects quiescence
- **Pr1...PrP (compute processors)**: Each has EC objects (neurons), CMC (communication control), CPC (computation control)
- **Static neural mapping**: neurons are assigned to processors; synapses may cross processors
- **MPI packets**: each carries events (CM events) + sender's clock array (for DAMNED 2006/2009)
- **Key insight**: No barrier, no central scheduler; order emerges from event causality + clock piggybacking

**Scale:**
- Works from 2 to many processors
- Each processor owns ~N neurons (static partition)
- Bidirectional MPI links (full mesh)

**Critical detail (red box):**
> "Order emerges from event causality + piggy-backed clock arrays."
> No synchronization barrier. No central scheduler.

### 02_processor_internals.png — Inside One Processor

**What it shows:**
The infinite loops running inside `Pr_p`:

**CMC — Communication & Control (green box, left)**:
1. **Message reception**: incoming MPI packets → insert CM events into queue, update `Clk[p]` (peer's last-known emission time)
2. **Emission control**: pop top CP event, check emission guard, send (if authorized) → create MPI packets with events + Clk[p]

**CPC — Computation Control (blue box, middle)**:
1. **Computation starter**: pop top CM event (by impact stamp), check computation guard, launch EC thread
2. **Result collector**: reap finished threads, insert new CP events

**EC Threads (dashed box, right)**:
- One thread per neuron computation
- Integrate impact weight into neuron potential
- If potential ≥ θ: forecast a spike (CP event with timestamp)

**Local state (red box, right)**:
- `et_p`: last emission timestamp (sign indicates CP queue empty)
- `pt_p`: last processing timestamp (sign indicates CM queue empty)
- `Clk[p]`: known emission times of all peers
- `nbth_p`: count of active EC threads

**Flow:**
```
Incoming MPI → [CMC Step 1] → CM queue
                                    ↓
                              [CPC Step 1] → EC thread
                                    ↓
                              [EC] → forecast spike
                                    ↓
                              [CPC Step 2] → CP queue
                                    ↓
                              [CMC Step 2] → MPI out
```

### 03_event_lifecycle.png — Temporal Ordering

**What it shows:**
How an event (a spike or impact) travels through the system and when it's committed.

**States:**
1. **Forecast** (at source processor): neuron crosses threshold → CP event created (Ni, impact_stamp, `crt=false`)
2. **Certification** (in computation): all inputs processed → `crt=true` (every impact up to this timestamp has been accounted for)
3. **Emission** (CMC step 2): guard authorizes → emit to targets
4. **Transit** (on wire): MPI packet in flight (subject to latency)
5. **Reception** (CMC step 1): packet arrives → unpack CM events, update clock
6. **Queued** (in target processor): CM event waiting in queue
7. **Computation** (EC thread): impact integrated into potential
8. **Committed** (when emission is final): the distributed trace records this spike

**Ordering rules:**
- Inhibition before excitation (at equal timestamps)
- Strictly causally ordered (no message overtakes an earlier one on the same link)

### 04_delayed_firing_problem.png — The Core Issue

**What it shows:**
The straggler scenario in detail:

**Network:**
- 2 processors, 3 neurons (all sources), 1 target (N0, threshold=4)
- Synapses: N1→N0 (+2, delay 3), N2→N0 (+2, delay 2), N3→N0 (-3, delay 1)
- All impacts hit N0 at logical time 3

**Sequential (ground truth):**
- At t=3: receive +2 (potential=2), +2 (potential=4, fire!), -3 (too late, still fires)
- Result: N0 fires at t=3

**Distributed (async):**
- Pr1 (owns N0): receives +2 from N1 (delay 3) → queued at t=3
- Pr1: receives +2 from N2 (delay 2) → queued at t=3
- Pr2 (owns N3): fires -3, sends it to Pr1 (MPI)
- **Race**: If Pr1 processes both +2 before the -3 wire message arrives, it commits N0 firing
- Result with straggler: N0 fires (wrong), ground truth says no

**Why the paper's original 2006 guard fails here:**
The guard at Pr1 checks: `if (et[Pr2] <= 0)` meaning "peer Pr2 looks idle, safe to emit". But Pr2 is NOT idle—it has the -3 in its CP queue, not yet emitted. The guard's disjunction doesn't enforce that Pr2's LBTS (lower bound) has passed the timestamp. Hence: straggler.

### 05_emission_authorization.png — The Guard (2006 vs 2009)

**What it shows:**
Side-by-side comparison of when a processor is allowed to emit.

**2006 Guard (red, left):**
```
if (st_i == et_m)                        // same as last emission?
    authorize
if (crt)                                 // certified (all inputs processed)?
    authorize
if (local_clear(n, st_i))                // local queue past this timestamp?
    authorize  ← BUG: ignores peer bounds
return false  // block otherwise
```

**2009 Guard (green, right):**
```
if NOT local_clear(n, st_i)
    return false  // must be locally clear first
if NOT (st_i < Clk[m] for ALL peers m)
    return false  // every peer's LBTS strictly past st_i
return true  // sound authorization
```

**Key difference:**
- 2006: disjunction of independent checks (any true → emit)
- 2009: conjunction + strict inequality (all true AND st < Clk → emit)

**Why 2009 works:**
By CMB, if `st_i < Clk[m]` for all peers, no earlier event from any peer can arrive. Combined with local clarity, emission is safe.

### 06_computation_authorization.png — When to Start Computing

**What it shows:**
When is it safe to pop a CM event and spawn an EC thread?

**2006 Guard:**
```
if (top_cm.st < pt[self] AND no active thread on this neuron)
    authorize computation
```

**2009 Guard:**
```
if (top_cm.st < min(Clk[m] for all m) AND no active thread on this neuron)
    authorize computation
```

**Difference:**
- 2006: uses local processing time as proxy
- 2009: uses peers' LBTS (strict bounds)

**Rationale:**
Don't start computing on an event until you're sure no earlier event from a peer can arrive. The 2009 guard waits for all peer bounds to pass the event's timestamp.

### 07_clock_synchronization.png — Bound Propagation

**What it shows:**
How `Clk[p]` (processor p's knowledge of peer's emission bounds) gets updated.

**Mechanism:**
1. Every MPI message carries `Clk[sender]` (sender's last-known peer bounds)
2. Receiver merges: `Clk_local[sender] := max(Clk_local[sender], Clk_received[sender])`
3. Broadcast: every few ticks (or on change), send null messages carrying just `Clk`, no events

**2006 issue:**
- Null messages might not happen or might be delayed
- A processor might believe a peer is idle when it has just emitted (stale clock)

**2009 fix (CMB):**
- Null messages are mandatory and timely (piggyback on every message or broadcast periodically)
- Guarantees: `Clk[m]` is never stale by more than lookahead d_min

**Lookahead:**
- The minimum synaptic delay (d_min) in the network
- If all delays are ≥ d_min, an event at time t won't land before t+d_min
- Clock can safely advance by d_min from a peer's last emission

## Diagrams 12–13: Comparisons & History

### 12_2006_vs_2009_algorithm_diff.png — Detailed Change Log

**What it shows:**
Every defect in 2006 and what 2009 fixed.

**2006 (red, left):**
- Lookahead d_min used ONLY for the certification flag (wrong scope)
- Guard uses bare CP queue empty, no temporal bound
- Pr0 holds T, broadcasts on TIME-OUT (anti-deadlock), fabricates `et_p(0) ← T-1`
- STL priority queue: O(n log n) insert/delete (naive)
- No ordering rule for simultaneous inhibit/excite
- Paper cited Ferrsch [22], Grassmann [23], never cited Chandy-Misra

**2009 (green, right):**
- d_min is the advance rule: `de = [t_i] + d_min` (when can peer next emit)
- Guard uses true LBTS: minimum over own CP queue + peer clocks + lookahead
- CMB: Clk[sender] sent every message, broadcast without events (null messages)
- Delayed queue: circular calendar O(1) worst-case insert/access (Pr0 TIME-OUT gone)
- Two-key ordering: inhibition before excitation at equal timestamp
- Chandy-Misra cited, null messages renamed "CMB"

**Status tags:**
- 🟢 FIXED — kills the counterexample
- 🟡 OPTIMIZED — performance improvement
- 🟠 ADDED — new insight
- 🟣 NAMED — terminology clarification

**Coverage comment (red box):**
> "The xYiu reprint is the freely reachable one; it's BROKEN. The EANN paper (paywall behind a Springer paywall) has all corrections. The corrected algorithm sits behind a Springer paywall. Still unverified: C++ source, which does not appear to be online."

### 13_trajectory_and_fate.png — Publication & Citation History

**What it shows:**
How DAMNED went through the world: where it was published, who cited it, what happened to it.

**The DAMNED line (blue boxes, center):**
- **2005 arXiv cs/0512018** (PDCN 2006): Original publication, uploaded as preprint
- **2008 Mouraud PhD thesis, Lyon**: Advisor Paugam-Moisy, defended 30 Sep
- **2009 EANN**: Corrected algorithm (Springer paywall, CCIS 43, pp. 359-370), full speedups on 35-node cluster

**The missed gate (red boxes, left):**
- **2007 Brette et al.**: J Comp Neurosci 23:349–398; "THE canonical simulator survey". Scope: freely available, open-source, documented simulators
- **DAMNED not included** (neither open-source in 2007 nor documented)
- **Later**: DAMNED was never mentioned in any major survey again

**What won (green boxes, right):**
- **2005 Morrison et al.** → **NEST** (Brian, GeNN, NEURON-parallel, CAULism, Arbor, SpinNaker)
  - All clock-driven or hybrid
  - All batched, all keep global synchronisation step
- **2010 ICANN**: saccadic eye-movement model (superior colliculus) — an application
- **Same lookahead insight as DAMNED 2009**: opposite conclusion about the barrier
  - NEST batches every d_min (keeps barrier, synchronisation optimised)
  - DAMNED removes barrier entirely (buffer spikes, broadcast clocks)

**Citation trajectory (blue box, bottom):**
- Indexed by DBLP, HAL, ADS, Semantic Scholar
- **Findable**: just NOT used

**Caveats (cyan box):**
- Semantic Scholar: count was not exposed to search (qualitative: indexed, self-cited, listed as background)
- Exact citation count: unknown; check Google Scholar directly
- ADS (Astrophysics Data System): automated access, non-canonical literature

**The verdict (gold box):**
> "DAMNED spent its whole complexity budget removing the synchronisation barrier. NEST observed that with lookahead d_min you can batch d_min-worth of spikes into one collective — at which point the barrier costs almost nothing, and you get O(1). Algorithm that is trivially correct and trivially debuggable instead of a distributed clock-array protocol whose core property is non-obvious."

> "DAMNED's two levels of parallelism survived, through: thread-core inside a node + MPI across nodes is exactly NEST's hybrid parallelization strategy today. The idea that won was hybrid parallelism — and DAMNED had it first, and got no credit, because nobody read it."

## How to Use These Diagrams

### For Understanding

**Start here (in order):**
1. `01_system_architecture.png` — "What is the big picture?"
2. `02_processor_internals.png` — "What does each processor do?"
3. `03_event_lifecycle.png` — "How does a spike move through the system?"
4. `04_delayed_firing_problem.png` — "Why is this hard?"
5. `05_emission_authorization.png` — "How do we prevent straggler mistakes?"
6. `06_computation_authorization.png` — "When is it safe to compute?"
7. `07_clock_synchronization.png` — "How do clocks stay in sync?"

**Then explore context:**
- `12_2006_vs_2009_algorithm_diff.png` — "What changed and why?"
- `13_trajectory_and_fate.png` — "How did DAMNED get here? What happened after?"

### For Teaching

**Lecture 1** (30 min): Diagrams 01–03
- System architecture, processor internals, event lifecycle
- Goal: Intuition for distributed SNN simulation

**Lecture 2** (30 min): Diagrams 04–05
- The straggler problem, the guard, why 2006 fails

**Lecture 3** (30 min): Diagrams 06–07
- Computation authorization, clock propagation, why 2009 works

**Bonus** (20 min): Diagrams 12–13
- Algorithm evolution, publication fate, lessons learned

### For Presentation

Print high-resolution PNGs (01–07 in sequence) for a conference poster or research seminar. They tell a complete story without text (though captions help).

## Integration with Other Artifacts

These diagrams complement:
- **`Damned.go`**: See how each processor's select loop maps to diagram 02
- **`reference/damned_ref.py`**: The predicates in the code match diagrams 05–06
- **`formal-verification/DamnedFull2.tla`**: The state machine corresponds to diagrams 02–03
- **`examples/viz.html`**: Watch diagram 03 unfold in real time

## Technical Notes

### Dark Theme Design

All diagrams use dark background (#0d1117 GitHub dark mode) with:
- Blue: Pr processors, data flow
- Green: Correct/authorized actions
- Red: Errors, violations, problems
- Purple: Formal proofs
- Gold: Lessons learned

High contrast on projectors and printed media.

### Numbering Scheme

01–07: Algorithm layers (system → clock)  
08–11: Formal correctness (proofs folder)  
12–13: Context (changes, history)

The sequence 01→02→...→13 is a traversal from implementation to publication.

## See Also

- [`../README.md`](../README.md) — Main documentation
- [`../Damned.go`](../Damned.go) — Implementation (cross-reference with diagram numbers)
- [`../formal-verification/`](../formal-verification/) — Proofs (diagrams 08–11)
- [`../reference/`](../reference/) — Reference implementation (predicates in diagrams 05–06)

## Questions?

Each diagram is self-contained but best understood in sequence. If one looks confusing:
1. Re-read the caption
2. Cross-reference with the code or spec
3. Trace an example event through the diagram
4. Compare to the sequential oracle in `reference/damned_ref.py`

---

**Bottom Line**: These diagrams are the visual spec. Read them in order, and you've learned the algorithm. Read them out of order, and you'll understand why the straggler is hard.
