# Formal Correctness & Proof Diagrams

This folder contains 4 diagrams that visualize the **mathematical proof structure** of DAMNED: how safety properties are established, what the counterexample looks like, how liveness is ensured, and how DAMNED maps to the Chandy-Misra-Bryant distributed algorithm framework.

These diagrams bridge the gap between implementation/testing and formal verification. They are the visual proofs.

## Diagram Series: 08–11 (Correctness Proofs)

### 08_safety_proof_structure.png — How the Proof Is Built

**What it shows:**
The logical structure of the safety argument for emission authorization.

**Top-level theorem (gold box):**
> "An authorized emission [N_i, st_i] can never be cancelled by a later spike"

This is the core correctness claim: once a spike is committed as firing, no later-arriving inhibitory impact can undo it.

**Three branches of proof (green, blue, red):**

**Branch 1 (green):** `st_i = et_p`
- "Same as last emission"
- **Proof**: By induction on previous emissions (last emission was safe by same argument)
- **Status**: Safe by construction

**Branch 2 (blue):** `crt = true`
- "Certified: every PROCESSED input lands after st_i"
- **Meaning**: All CM events on this neuron up to time st_i have been integrated (either increasing or decreasing the potential)
- **Proof**: If all inputs up to st_i have been seen, and the neuron fired, no later input can retroactively cancel it
- **Status**: NEEDED (for 2006 guard correctness — but 2006 doesn't enforce it strictly)

**Branch 3 (blue):** `st_i = pt_p`
- "Local processing already past st_i"
- **Meaning**: This processor has no more work scheduled before st_i
- **Proof**: If this processor won't process anything before st_i, and no peer can emit earlier than their last known time, then...
- **Status**: NEEDED (but incomplete without peer bounds)

**Central lemma (purple):**
> LEMMA No-Straggler(st_i):  
> No unprocessed or in-flight spike anywhere has a stamp below st_i − d_min_i

**This lemma claims:**
- "There is no event at or before st_i - d_min_i still pending (queued, running, or in flight)"
- "With lookahead d_min_i, all impacts that could land at or before st_i have already been queued"

**The proof of this lemma:**
- Branch 4 (orange): `nbth_p = 0 AND pt_p < 0 AND ∀m [st_i ≤ et_m ∨ et_m < 0]`
  - No active EC threads on this processor
  - Local processing is done (sign bit negative = idle)
  - For all peers m: either st_i ≤ et_m (peer's last emission is at or after st_i, so nothing earlier), OR et_m < 0 (peer idle, Clk[m] carries no meaningful info)

**Critical caveat (cyan box):**
> "The paper's pseudocode is terse. This criticism is stated so that it holds under BOTH readings of 'et_m ≤ 0'."
> 
> **Issue**: Does `et_m ≤ 0` mean:
> 1. "CP queue is empty (et_m magnitude is zero with sign bit empty)"?
> 2. "Peer looked idle to us (we haven't heard a recent bound update)"?
>
> The 2006 guard conflates these. The disjunction assumes one reading, but the proof needs both simultaneously.

**Red box conclusion:**
> "BOTH READINGS CONVERGE: The Pr0 time-out is not a patch on a sound algorithm. It IS the unsoundness — and simultaneously the only guarantee of progress. Soundness and liveness are traded, exactly as CMB predicts when null messages carry no lookahead."

This is the deep insight: the 2006 guard's disjunction tries to get soundness for free, but the only way it terminates (progress/liveness) is via Pr0's artificial time-out, which breaks soundness.

### 09_counterexample_trace.png — A Concrete Violation

**What it shows:**
A step-by-step trace showing exactly how the 2006 guard commits a spike incorrectly.

**States:**
- State 1–2: Both processors idle, Pr0 injects stimuli
- State 3–4: Pr1 & Pr2 process their respective stimuli
- State 5: Pr1 has the two excitatory events (+2, +2) queued; Pr2 emits the inhibitory event (-3)
- State 6–7: Messages in flight (Pr2's -3 is on the wire)
- State 8–9: Pr1 processes the two +2 events, potential reaches 4 ≥ θ
- State 10: Pr1 commits N0 firing at time t=3
- **State 11 (VIOLATION)**: Pr2's -3 arrives, but it's too late; N0 already fired

**Guard failure:**
At State 10, Pr1 checks: `et[Pr2] = 0` (Pr2 appears idle) → EMIT (incorrectly authorized)

But Pr2 was NOT idle; it had emitted the -3. The message was just in flight.

**Why this matters:**
This trace is the **counterexample** that TLC finds in 4 states. It's the constructive proof that 2006 is broken.

### 10_liveness_timeout.png — Deadlock Avoidance

**What it shows:**
How DAMNED 2006 ensures progress (liveness) despite the straggler hole.

**The problem:**
If a processor waits for a peer's bound to advance, but the peer is waiting for something else, and that something is waiting for the first processor... deadlock.

**Timeout mechanism (2006):**
```
Pr0 holds actual time T (wall-clock).
Every epoch: T += 1
Broadcast: "I will not inject anything earlier than T"
  → All processors merge this into Clk[0]
Pr0 fabricates: et_p(0) ← T-1
  → Signals that Pr0 won't speak at time < T
```

**Why it helps:**
By broadcasting the actual wall-clock time T (not logical time, but a moving target), Pr0 guarantees that Clk[0] keeps advancing. Even if compute processors deadlock, Pr0's timeout message unblocks them (they see Clk[0] advancing, so they can authorize emissions).

**The cost:**
- **Unsoundness**: Pr0's artificial `et_p(0) ← T-1` is not a real emission; it's a fabricated bound. If a compute processor reads this as "Pr0 has emitted at T-1", they might emit earlier events that conflict with real stimuli Pr0 hasn't sent yet.
- **Soundness recovery**: Only because Pr0 doesn't actually inject anything before T; the fabrication is pessimistic (conservative). But it's a hack.

**2009 fix (CMB):**
- No Pr0 time-out needed
- Null messages (just bounds, no events) ensure clocks advance without real emissions
- Liveness is guaranteed by CMB: if all processes follow the null-message protocol, deadlock is impossible (proven in CMB literature)

**Bottom line:**
The 2006 liveness is achieved via a Trojan horse (Pr0's fabricated time-out). The 2009 discipline achieves both safety AND liveness via null messages.

### 11_cmb_correspondence.png — DAMNED in the Design Space

**What it shows:**
How DAMNED 2006 and 2009 fit into the broader taxonomy of distributed synchronization algorithms, specifically Chandy-Misra-Bryant (CMB).

**CMB landscape (green boxes, top):**
- **per-CHANNEL clock**: one clock per directed channel (sender → receiver), synchronized independently
- **Lookahead > 0 in the advance rule**: `de = [t_i] + d_min` (a process cannot emit earlier than lookahead from its last emission)
- **NULL MESSAGES (t := 1)**: peer-to-peer "I have nothing to say before time t" messages
- **FIFO bound**: messages preserve order within a channel

**Grassmann safe window (blue box):**
- Computes a globally SAFE interval: every process stays within it, no causality violation
- "look-ahead QUERY messages" — a query protocol to discover safe intervals
- SOUND at the cost of query traffic

**DAMNED's position (center):**
Three layers:

**Layer 1 — Chandy-Misra-Bryant (conservative)**:
- per-channel clock, FIFO links
- Null messages (t := 1)
- **Principle**: If Clk[m] (sender m's lower bound) > time t, no event from m can arrive at or before t

**Layer 2 — DAMNED (optimistic inside neuron)**:
- Channels are FIFO ✓
- Per-processor clock (merges all peers' bounds into Clk[p])
- Lookahead d_min ✓
- Null messages with sender's full Clk array (not just d_min; centralized clock snapshot)
- **Optimization**: Inside the neuron, allow optimistic computation (early spike forecast) once all inputs up to some t are queued

**Layer 3 — Distributed neuron algorithms** (not shown):
- Some (like NEST, Brian) add explicit barriers (all processors pause every d_min steps)
- DAMNED removes the barrier (no sync point)
- **Trade-off**: Barrier means global clock is conservative (everyone agrees on time); no barrier means clock is broadcast (must be piggybacked)

**2006 vs 2009 positioning:**
- **2006**: Tries to be conservative at the emission boundary (Channel clock discipline) but uses a bare disjunction that doesn't enforce it
- **2009**: Fully adopts CMB (strict clock checks, null messages)
- **2009 optimization**: Doesn't need Grassmann's query protocol because lookahead + FIFO is already safe

**Not sound as written (red box, right)**:
> "The V'm range guard (V'm ≤ 0) is EVERY commitment event is conditional on 'peer looks idle'. But a peer can emit-and-delay, not having told us yet. The commitments event is NOT as written in every computation event...and breaks at deadlocks."
> "That is a controller with a modest job description. These is a deadlock! Always."

Translation: The 2006 guard's bare idle-check (`et_m ≤ 0`) doesn't enforce CMB's per-channel clock condition. Hence deadlocks, and only Pr0's time-out avoids them.

**SOUND correspondence (green box, bottom):**
The 2009 discipline enforces CMB rules:
- Local deadlock detection + recovery: each processor can detect when it's stuck (all event queues empty, waiting on peer bounds)
- FABRICATED clock advances: When Pr0's time-out is removed, the only signal that processes shouldn't deadlock is CMB's theorem: null messages guarantee forward progress

**Caveats about the barrier:**
> "No SOUND as written: Live, always."
> But the CPC guard (V'm ≤ 0) is EVERY commitment event... and it's controlled on deadlocks. That is a controller with a modest job description."

This cryptic note means: the 2006 guard tries to be a "modest controller" (local, no global sync) but its deadlock handling is via Pr0's time-out, which is neither modest nor provably sound.

## How to Read These Diagrams

### For Proof Understanding

**Start with 08:**
- Understand the theorem (top gold box)
- Trace through each branch
- Identify where 2006 fails (Branch 4: disjunction doesn't enforce No-Straggler lemma)

**Then 09:**
- See a concrete counterexample trace
- Count the states: 11 states to reach violation
- TLC finds this within seconds

**Then 10:**
- Understand the cost: deadlock avoidance via time-out
- See why 2009 is better: CMB null messages replace the hack

**Finally 11:**
- Understand the design space
- See DAMNED's place in distributed algorithms taxonomy
- Learn what CMB enforces that DAMNED 2006 misses

### For Verification

**If you're implementing DAMNED:**
- Diagram 08 tells you what to prove about each guard check
- Diagram 09 tells you a test case that must NOT occur
- Diagram 10 tells you liveness is nontrivial; don't skip it
- Diagram 11 tells you where in the literature to look for theory

**If you're model-checking:**
- Diagram 08 gives the proof structure (guides what invariants to check)
- Diagram 09 is your target: TLC should NOT find this trace
- Diagram 10 reminds you: check liveness (not just safety)
- Diagram 11 points to CMB literature for soundness proofs

### For Teaching

**Graduate seminar (1 lecture, 60 min):**
1. (15 min) Diagram 08: What does the safety proof assume?
2. (15 min) Diagram 09: What went wrong in 2006?
3. (15 min) Diagram 10: How do we fix liveness?
4. (15 min) Diagram 11: Where does DAMNED fit in the bigger picture?

## Integration with Other Artifacts

**Reference implementations:**
- `reference/damned_ref.py`: Exhibits the counterexample (diagram 09)
- `reference/Damned.tla`: Proves diagram 08 is false (finds violation)
- `reference/DamnedFixed.tla`: Proves diagram 08 is true for 2009 (no error)

**Formal verification:**
- `formal-verification/DamnedFull2.tla`: Full proof of diagram 08 with Exactness invariant
- `formal-verification/RESULTS_FULL.txt`: TLC output proving diagrams 08–09

**Algorithm analysis:**
- `analysis/05_emission_authorization.png`: Diagram 08 in code form
- `analysis/12_2006_vs_2009_algorithm_diff.png`: Context for why diagram 08 was needed

**Implementation:**
- `Damned.go`: Implements the guards shown in diagram 08
- Line-by-line correspondence with TLA+ specs (diagrams 08–09)

## Technical Notes

### Proof Rigor

These diagrams are **semi-formal**: they show the logical structure but not formal notation. For complete rigor, see:
- `formal-verification/DamnedFull2.tla` (formal TLA+ proof)
- Reference papers (Mouraud et al. 2009, Chandy & Misra 1979)

### Notation

- `et[p]`: emission time of processor p (signed; magnitude only when not idle)
- `pt[p]`: processing time (signed)
- `Clk[p]`: array of known emission times of all peers (clock array from CMB)
- `crt`: certification flag (all inputs up to this time have been processed)
- `nbth[p]`: number of active EC threads on processor p
- `d_min`: minimum synaptic delay (lookahead)
- `T`: actual (wall-clock) time, used by Pr0 time-out

### Color Scheme

- **Green**: Sound/correct parts of 2009 discipline or correct proof branches
- **Blue**: Formal concepts (CMB, Grassmann, etc.)
- **Red**: Errors, violations, unsound parts of 2006
- **Purple**: Formal predicates/lemmas
- **Gold**: Theorems, conclusions

## See Also

- [`../formal-verification/`](../formal-verification/) — Full TLA+ specs (executable proofs)
- [`../reference/`](../reference/) — Python sim + small TLA+ models (find counterexamples)
- [`../analysis/`](../analysis/) — Implementation-level diagrams (08 in code form = diagram 05)
- [`../Damned.go`](../Damned.go) — Guards correspond to diagram 08, branches

## Questions?

**"Why is 2006 broken?"**  
Diagram 08: the disjunction doesn't enforce the No-Straggler lemma. Branch 4 is never true when needed.

**"What's the counterexample?"**  
Diagram 09: 11 states, Pr1 commits N0 at state 10, Pr2's -3 arrives at state 11.

**"How does 2009 fix it?"**  
Diagram 11: By adopting strict CMB (per-channel clocks, null messages), the lemma is guaranteed true.

**"What's the cost of 2009?"**  
Diagram 10: Extra null messages (slight bandwidth increase), but no Pr0 time-out hack (cleaner code).

---

**Bottom Line**: These diagrams are the visual proof. Diagram 08 is the theorem, diagram 09 is the counterexample to 2006, diagram 10 shows the cost, diagram 11 shows the solution. Together, they constitute a complete correctness argument.
