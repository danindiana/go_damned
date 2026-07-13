# Reference Implementations: The Complete Conversation

This folder contains:
- **`damned_ref.py`** — Python executable reference (faithful reconstruction of §4 predicates)
- **`Damned.tla` / `DamnedFixed.tla`** — TLA+ abstract state machines (2006 guard vs 2009 fix)
- **`DAMNE_convo.txt`** — Complete technical conversation (analysis, findings, live reconstruction)
- **`RESULTS.txt`** — Model-checking and Python output summary
- **`14_reconstruction_results.png` / `15_tlc_verdict.png`** — Visual summaries of the findings

The conversation file is the authoritative reference. What follows is a digest organized by topic.

## The Paper in One Paragraph

Spiking neural nets are sparse in time (low average firing rate), so time-driven simulators like GENESIS/NEURON waste effort scrolling every neuron every Δt. Event-driven simulation fixes that but is normally sequential. DAMNED makes EDS parallel on two axes at once:
- **Coarse grain**: the topology is statically mapped across P processors talking over MPI
- **Fine grain**: each processor runs one thread per currently-computing neuron

The interesting contribution is what's *absent* — **no synchronization barrier** and **no farmer/controller processor**. Ordering is enforced locally, from event causality plus clock arrays that piggyback on the spike packets processors were sending anyway.

## The Hard Problem: Delayed Firing & Stragglers

A spiking neuron forecasts its next firing time from the inputs it has integrated so far. But an **inhibitory spike arriving later, with a shorter delay, can land at the soma earlier and cancel that forecast**.

**Example:**
- Neuron N0 (threshold=4) receives two excitatory spikes (+2 each) impacting at t=3
- A remote inhibitory spike (-3) is in flight, also impacting at t=3
- If N0 is committed at t=3 before the inhibition arrives, it fires incorrectly
- Sequential ground truth: N0 never fires (inhibition cancels the threshold crossing)
- 2006 guard (buggy): commits N0 anyway (straggler strikes)

This is the **straggler problem**: a slow message invalidates a decision already made. The paper's 2006 guard has a hole; the 2009 fix closes it via Chandy-Misra-Bryant discipline.

## Three Defects in the 2006 Guard (and How 2009 Fixed Them)

### Defect 1: Lookahead Used Only for Certification

**2006:** `d_min` (minimum synaptic delay) appears only in the certification flag `crt`. It marks "I've processed enough inputs; nothing earlier can cancel me."

**Problem:** The lookahead doesn't constrain advancement. A processor can emit without knowing peer bounds.

**2009 Fix:** The emission guard is `de ≤ |te_j| + δmin` for every peer. Lookahead now gates advancement, exactly as CMB requires.

### Defect 2: Idle-Peer Flag Conflates Idleness with Temporal Advancement

**2006:** `et_m ≤ 0` means "peer's CP queue is empty" and is treated as vacuously safe. **But idleness ≠ temporal advancement.** An idle processor may hold a low-stamped, unprocessed event in its CM queue.

**Example (the counterexample):**
```
Pr1 at t=3: "I can emit N0 because Pr2 looks idle (et_2 = -3)"
BUT Pr2 fired an inhibitory spike at t=2, on the wire, not yet seen by Pr1
Result: N0 commits incorrectly; ground truth says N0 never fires
```

**2009 Fix:** When a processor's outgoing queue empties, it computes a real lower bound: `te_src ← −|tp_src − δmin|` (or `INF` if truly nothing to do). This is the minimum stamp the processor can still emit, derived from its processing time + lookahead — not a bare "empty" flag.

### Defect 3: Pr0's Time-Out Fabricates Clocks

**2006:** To avoid deadlock, Pr0 holds global time `T`, broadcasts to all, and breaks deadlocks by setting `et_p(0) ← T−1` for every processor. **But this is not a real emission; it's a fabricated clock**, telling processors "don't wait for me." A processor genuinely at stamp 4 is now advertised as being at T−1=8.

**Problem:** The same violation reappears, one layer down. The time-out is not a patch; **it IS the unsoundness, and simultaneously it is the only thing guaranteeing progress.**

**2009 Fix:** Pr0 is demoted to a "front-end task." Null messages (clock-only messages) replace the time-out, carrying the receiver's real LBTS via `clock_sent[]` booleans. Deadlock avoidance is provably sound via CMB.

## The Counterexample (Mechanical Proof)

Both the Python reconstruction and TLA+ model checking produce this trace:

| Step | State | What Happens |
|------|-------|---|
| 1 | Init | Pr0 injects stimuli: two excitatory to Pr1, one inhibitory to Pr2 |
| 2 | | Pr2 fires the inhibitory spike, emits it, `et_2 ← 2` |
| 3 | | Pr1 processes the first two excitatory, potential reaches 4 ≥ θ, forecasts spike |
| 4 | | Pr1 checks emission: `et_2 ≤ 0` → **passes via bare idle check** |
| 5 | | **Pr1 COMMITS N0 at t=3** ← *violation point* |
| 6 | | Inhibition arrives: impact on N0 at t=3 |
| 7 | | Too late; N0 already fired. Exactness violated. |

**2009 guard:** Pr1 checks `3 < kb[2]` (3 strictly less than peer 2's bound). Pr2's bound hasn't passed 3 yet (it's 2 + d_min = 3, not > 3), so **Pr1 BLOCKS**. Once Pr2 processes the inhibition and advances its bound to 4, Pr1 can safely commit.

## Verification Results

### Python `damned_ref.py`

| Scenario | 2006 Guard | 2009 Discipline |
|----------|-----------|---|
| **Cancellation** | 200/200 diverge, spurious `(3,0)` | 0/200 match ground truth |
| **Figure 1** (paper's own example) | matches | matches |
| **Random 15-neuron, 3-proc** | 228 vs 195 spikes, 120/120 diverge | All match |

### TLA+ Model Checking (Exhaustive at P=2, MaxT=2)

| | 2006 Guard | 2009 Discipline |
|---|---|---|
| **Exactness** | Violated at depth 14 | Satisfied over 197,246 states |
| **Verdict** | Error found | No error found |

### Go Implementation (`../Damned.go`)

At zero latency:
- 2006: 0/300 mismatches (Go scheduler doesn't hit the race)
- 2009: 0/300 mismatches (sound)

With `-latency=2ms` (realistic network condition):
- 2006: 120/120 mismatches, spurious `[3 0]`, oracle fires
- 2009: 0/120 sound

**Lesson:** The straggler is real, but concurrency testing alone won't find it reliably. Model checking forces the interleaving; latency makes it probable in practice.

## Why 2009 = Chandy-Misra-Bryant (with Piggybacking)

| CMB Concept | DAMNED 2006 | DAMNED 2009 | Status |
|---|---|---|---|
| per-channel clock | per-process `et_m` in `Clk(p)` | real LBTS bound | **weakened → fixed** |
| lookahead L > 0 | used only for `crt` | used in emission guard | **scoped correctly** |
| null message | absent (time-out instead) | `clock_sent[]` broadcast | **added** |
| deadlock avoidance | fabricated clocks | proven via CMB | **made sound** |

**Conclusion:** Every independent attempt to repair the guard converged, under machine checking, onto the exact structure of Chandy–Misra–Bryant.

## Publication Fate: Distribution Failure with Real Consequences

### The Timeline
- **2005** arXiv preprint (cs/0512018)
- **2006** PDCN, Innsbruck — the ORIGINAL, with the hole
- **2008** Mouraud's PhD thesis (Lyon) — likely contains corrections
- **2009** EANN — the CORRECTED algorithm, full rewrite (Springer paywall)
- **2010** ICANN — an application paper (saccadic eye movements)
- **After 2010:** nothing

### The Missed Gate
**Brette et al., 2007,** *Simulation of networks of spiking neurons: a review of tools and strategies*, J. Comput. Neurosci. 23:349–398. The canonical 22-author survey, scope: "freely available, open source and documented."

**DAMNED was NOT included.** Published the year before, but in 2007 it was none of those three things. Result: every reader who used Brette 2007 as their map of the field never encountered it. That survey has ~1000 citations.

### Why the Barrier-Free Idea Didn't Spread

**NEST's insight (same era):** Since `d_min` means nobody can influence anybody for `d_min` time, don't remove the barrier — *widen the step*. Buffer spikes, send in batches every `d_min`. Barrier still there, but amortized over thousands of spikes.

**DAMNED** spent its entire complexity budget deleting the barrier. **NEST** observed the barrier is the cheap part and bought instead an algorithm that is trivially correct and trivially debuggable. **That trade decided the field.** NEST, Brian, GeNN, NEURON-parallel, CARLsim, Arbor, SpiNNaker — all batched, all keeping a global sync step.

**DAMNED's surviving contribution:** Two levels of parallelism — threads within a node + MPI across nodes — is precisely what NEST now calls hybrid parallelization. DAMNED had it first (2006), argued for it explicitly, got no credit. The idea it staked its identity on (barrier-freedom) is the one that died.

## How to Use This Folder

### Quick Start: See the Bug

```bash
# Sequential ground truth (always correct)
python3 damned_ref.py --scenario cancel

# Distributed 2006 guard (breaks reliably with adversarial scheduler)
python3 damned_ref.py --scenario cancel --adversarial --max-tries 50

# 2009 fix (always matches)
python3 damned_ref.py --scenario cancel --reps 200 --seed 0
```

### Model Checking: Prove It

```bash
# Requires: TLA+ Toolbox or command-line TLC

# 2006 guard: find violation in 4 states
java -cp tla2tools.jar tlc2.TLC -config DF2_small_2006.cfg Damned.tla

# 2009 discipline: exhaustive, no error
java -cp tla2tools.jar tlc2.TLC -config DF2_small.cfg DamnedFull2.tla
```

Expected output (2006):
```
Error: Invariant Exactness is violated.
7 states generated, 6 distinct states found, 1 states left on queue.
The depth of the complete state graph search is 4.
```

Expected output (2009):
```
Model checking completed. No error has been found.
740431 states generated, 197246 distinct states found, 0 states left on queue.
The depth of the complete state graph search is 45.
```

### Live Execution: Watch the Organism

From `../Damned.go`:

```bash
# Correct 2009 guard
go run ../Damned.go -guard=2009 -scenario=cancel -latency=2ms -reps=100

# Buggy 2006 guard (watch it diverge)
go run ../Damned.go -guard=2006 -scenario=cancel -latency=2ms -verbose

# Interactive trace (opens in browser)
go run ../Damned.go -guard=2006 -latency=2ms -trace=trace.json
# Then open ../examples/viz.html and load the JSON
```

## Key Findings (TL;DR)

1. **The 2006 guard is provably unsound**: Python reconstruction, TLA+ model checking, and Go execution all find the same counterexample (spurious `[3,0]` when ground truth is no spike).

2. **The mechanism is subtle**: Idleness `et_m ≤ 0` is conflated with temporal advancement. A peer advertises "I'm done" when its own queue empties, but a message in flight can resurrect it. The guard never learns about that.

3. **The 2009 fix is correct**: Same invariants, same model, flag changed from `FIX=FALSE` to `FIX=TRUE`, and TLC finds no errors across 197K+ states.

4. **The fix is CMB**: Every repair attempt converged on Chandy–Misra–Bryant discipline. Not coincidence — CMB is the *only* sound solution.

5. **Distribution failure hid it**: The corrected 2009 version is behind a Springer paywall. The arXiv preprint (the broken version) is what people find today. Real consequences.

6. **The idea won anyway**: DAMNED's two-level parallelism (threads + MPI) is exactly what NEST calls hybrid parallelization today. The barrier-free aspect lost; the hybrid parallelism survived — just in NEST's framework instead.

## Files

| File | Purpose |
|------|---------|
| `damned_ref.py` | Python sim: 2006 predicates, ground-truth oracle, controllable scheduler |
| `Damned.tla` | 2006 guard abstract model (finds violation) |
| `DamnedFixed.tla` | 2009 discipline abstract model (sound) |
| `DF2_small.cfg` | Config for 2009 (exhaustive, no error) |
| `DF2_small_2006.cfg` | Config for 2006 (finds violation) |
| `RESULTS.txt` | Python and TLC output consolidated |
| `14_reconstruction_results.png` | Build process and test outcomes (visual) |
| `15_tlc_verdict.png` | Guard semantics and model-checking verdict (visual) |
| `DAMNE_convo.txt` | Complete technical conversation (authoritative) |
| `README_REFERENCE.md` | Original folder README (implementation guide) |
| `README.md` | This file (digest of the conversation) |

## See Also

- [`../README.md`](../README.md) — Main documentation
- [`../Damned.go`](../Damned.go) — Go implementation validated against this reference
- [`../formal-verification/`](../formal-verification/) — Full TLA+ model (P processors)
- [`../analysis/`](../analysis/) — Visual diagrams (01–07 algorithm layers)
- [`../proofs/`](../proofs/) — Proof structure diagrams (08–11)

## Questions?

Start with the **Quick Start** commands above to see the bug for yourself. Then read `DAMNE_convo.txt` section by section:

1. **"The paper in one paragraph"** — context
2. **"The hard problem it solves"** — why this matters
3. **"The safety claim — and a counterexample"** — the bug, hand-proved
4. **"The executable reference (damned_ref.py)"** — Python validation
5. **"The model checker (Damned.tla / DamnedFixed.tla)"** — TLC verdict
6. **"The trajectory"** — publication fate and citations

The conversation is a complete, unflinching technical artifact. Read it.

---

**This folder is the complete record of how a research paper's algorithm was deconstructed, proven unsound, fixed, and verified — from hand-proof to executable reference to formal model to production code.**
