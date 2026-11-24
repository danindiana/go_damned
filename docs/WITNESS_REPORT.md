# Substrate witness: the group word problem on an event-driven SNN

`scenario=word` turns `go_damned` from a correctness study of a simulator into a
Tier 1 substrate witness. It runs the word problem for a finite group on a
**constant-size** spiking network and checks the answer against both a spiking
oracle and a pure group-theoretic one.

## 1. Why this is the right witness

Barrington (1986): bounded-width branching programs over `S5` compute exactly
`NC1`, and the word problem for any **non-solvable** group is `NC1`-complete
under `AC0` reductions. For solvable groups it collapses into `ACC0`.

So "can this substrate do algebraic composition" is not a matter of taste. It is
the question of which group it can track products in, and the dividing line is
solvability. This also disqualifies `PARITY` as a witness for a second reason
beyond the usual one: `PARITY` is `Z2`, maximally solvable.

The claim under test is the one in the convergence cone — that recurrence
transcends `TC0` through *temporal* state rather than *spatial* parallelism.
The executable form of that claim is: an `NC1`-complete function computed with a
neuron and synapse count independent of `n`, and depth linear in `n`.

Measured, and constant across every `n` from 8 to 64:

```
neurons = 364    synapses = 1080    depth = 2n
```

That is the witness. Resources fixed, capability scaling with time.

## 2. Encoding

Neuron model available: integer potential, **no leak**, fires at `pot >= theta`,
resets to 0 on fire, at most one fire per timestep, and at equal timestamps all
inhibitory impacts are applied before excitatory ones.

The no-leak property is the whole difficulty. A coincidence detector with two
sparse subthreshold inputs accumulates across steps and eventually fires
spuriously; no choice of weights or thresholds avoids this, because the drift is
structural. The construction below solves it without modifying the neuron model.

**Layers** (`|G|` = group order, `|S|` = generator count):

| layer | count | theta | role |
|---|---|---|---|
| `s_sigma` | \|G\| | 2 | one-hot current group element |
| `c_{pi,g}` | \|G\|·\|S\| | 2 | conjunction: state `pi` AND symbol `g` |
| `in_g` | \|S\| | 1 | input line per generator |
| `FL` | 1 | 1 | flush clock |
| `IH` | 1 | 1 | cancel clock |

**Per-step schedule.** Step `k` occupies ticks `[2k, 2k+2)`:

```
t = 2k     s_sigma fires (current state); in_{g_k} fires (current symbol)
t = 2k+1   c_{sigma,g} sees +1 (state) +1 (input) = 2 -> FIRES
           off-diagonal c neurons hold a residual +1
           FL fires
t = 2k+2   s_{g.sigma} receives +2 from the winner -> FIRES. next state.
           FL's +2 reaches EVERY c: each fires (0 or 1, +2 >= theta) and
           resets to 0. conjunction layer clean for step k+1.
           IH fires
t = 2k+3   flush echo: every s receives +2 from each of its |S| incoming
           conjunctions = +2|S|, a CONSTANT because every element has
           exactly one predecessor per generator. IH delivers exactly
           -2|S| at the same tick; inhibition sorts first, so each s goes
           0 -> -2|S| -> 0 and never crosses threshold. net zero.
```

The flush guarantees every conjunction neuron resets every step, which kills the
accumulation. The cancellation makes the flush harmless to the state layer.

**Why this makes it a witness rather than a demo.** The cancellation is an
inhibitory pulse that must arrive before excitation carried on other processors.
If a processor commits the excitatory sum without proof that the inhibition
cannot still be in flight, every state neuron fires at once and the tracked
group element is destroyed. Guard soundness and algebraic correctness are the
same property here. The complexity task and the distributed-correctness task are
not two experiments sharing a codebase; they are one measurement.

## 3. Groups

| group | order | gens | solvable | word problem | role |
|---|---|---|---|---|---|
| `S5` | 120 | 2 | **no** | `NC1`-complete | witness |
| `Z120` | 120 | 2 | yes (abelian) | `ACC0` | matched control |

Same order, same generator count, therefore byte-identical network topology and
resource budget. Any difference cannot be attributed to network size.

Smaller instances (`S3`, `S4`, `Z6`, `Z24`) are available for cheap shakeout.

## 4. Validation of the encoding

Before any distributed run, the sequential oracle `sequentialEDS` is compared
against `wordProduct`, a direct group-theoretic computation. `encoding=true` in
every cell, both groups, every `n` from 4 to 64. The spiking construction
computes the group product it claims to.

## 5. Results

`P=3`, latency 0. `n <= 32`: 5 words x 20 reps = 100 runs per cell.
`n = 64`: 5 words x 10 reps = 50 runs per cell.

| group | n | guard | train mismatch | **wrong product** | one-hot broken | soundness flag |
|---|---|---|---|---|---|---|
| S5 | 8 | 2006 | 100/100 | **100/100** | 100/100 | 100/100 |
| S5 | 8 | 2009 | 2/100 | 0/100 | 0/100 | 0/100 |
| S5 | 8 | 2009L | **0/100** | **0/100** | 0/100 | 0/100 |
| S5 | 16 | 2006 | 100/100 | **100/100** | 100/100 | 100/100 |
| S5 | 16 | 2009 | 3/100 | 2/100 | 2/100 | 0/100 |
| S5 | 16 | 2009L | **0/100** | **0/100** | 0/100 | 0/100 |
| S5 | 32 | 2006 | 100/100 | **100/100** | 100/100 | 100/100 |
| S5 | 32 | 2009 | 57/100 | **44/100** | 48/100 | 0/100 |
| S5 | 32 | 2009L | **0/100** | **0/100** | 0/100 | 0/100 |
| S5 | 64 | 2006 | 50/50 | **50/50** | 50/50 | 50/50 |
| S5 | 64 | 2009 | 38/50 | **29/50** | 33/50 | 0/50 |
| S5 | 64 | 2009L | **0/50** | **0/50** | 0/50 | 0/50 |
| Z120 | 8 | 2006 / 2009 / 2009L | 100 / 3 / 0 | 100 / 1 / 0 | 100 / 1 / 0 | 94 / 0 / 0 |
| Z120 | 16 | 2006 / 2009 / 2009L | 100 / 6 / 0 | 100 / 5 / 0 | 100 / 5 / 0 | 93 / 0 / 0 |
| Z120 | 32 | 2006 / 2009 / 2009L | 100 / 6 / 0 | 100 / 6 / 0 | 100 / 6 / 0 | 99 / 0 / 0 |
| Z120 | 64 | 2006 / 2009 / 2009L | 50 / 33 / 0 | 50 / 31 / 0 | 50 / 31 / 0 | 50 / 0 / 0 |

Rerun on 2026-11-24, same settings (`P=3`, latency 0, 5 words x 20 reps = 100
runs per cell; n = 64 not rerun). Full output in
[`results/2026-11-24-sweep.txt`](results/2026-11-24-sweep.txt).

| group | n | guard | train mismatch | **wrong product** | one-hot broken | soundness flag |
|---|---|---|---|---|---|---|
| S5 | 8 | 2009 / 2009L | 8 / **0** | 3 / **0** | 5 / 0 | 0 / 0 |
| S5 | 16 | 2009 / 2009L | 22 / **0** | 14 / **0** | 17 / 0 | 0 / 0 |
| S5 | 32 | 2009 / 2009L | 37 / **0** | 27 / **0** | 35 / 0 | 0 / 0 |
| Z120 | 8 | 2009 / 2009L | 8 / **0** | 2 / **0** | 2 / 0 | 0 / 0 |
| Z120 | 16 | 2009 / 2009L | 25 / **0** | 18 / **0** | 18 / 0 | 0 / 0 |
| Z120 | 32 | 2009 / 2009L | 24 / **0** | 18 / **0** | 18 / 0 | 0 / 0 |

Network size was 364 neurons and 1,080 synapses in every cell, and the encoding
check passed in every cell. The `2009` failure rates differ from the first run
(27% against 44% wrong products for S5 at n = 32); they depend on scheduling and
should not be quoted as stable rates. `2009L` was 0 in every cell of both runs.

`2009L` is the corrected guard introduced by this work. See
`DEFECT-local-straggler.md`.

## 6. What the numbers say

**The witness passes, under a corrected guard.** `2009L` computes the `S5`
product exactly, over every interleaving tested, at every length up to 64, with
364 neurons and 1080 synapses. An `NC1`-complete function on a substrate whose
spatial resources do not grow with the input.

**The `2009` guard, as reconstructed here, is not sound.** It returned the
wrong group element in 44% of runs at `n=32` and 29% at `n=64` in the first
sweep, and 27% at `n=32` in the rerun. Reduced to a four-neuron
counterexample; see the defect report.

**Failure rate tends to rise with `n` under `2009`.** S5 wrong products were
0%, 2%, 44%, 29% at `n` = 8, 16, 32, 64 in the first sweep and 3%, 14%, 27% at
`n` = 8, 16, 32 in the rerun. Each step is another opportunity for the race,
and one corruption destroys the answer, so failures accumulate with length.
The trend is not strictly monotone (n = 64 in the first sweep, Z120 at 16
against 32 in the rerun), but longer words are the more reliable way to
expose the bug.

**The control behaves identically.** `Z120` and `S5` show the same rates within
noise. This is the correct outcome and worth stating plainly rather than
dressing up: the substrate does not care about solvability. The complexity claim
rests on Barrington's theorem plus the measured resource profile, not on the
network doing anything different for a non-solvable group. What the control
establishes is that the resource profile is genuinely matched, so the `NC1`
claim cannot be attributed to `S5` having been given a bigger network.

**The soundness flag is blind.** 0 in every `2009` failure. Only oracle
comparison caught them.

## 7. What this discharges, and what it does not

**Discharges: the substrate witness.** A stateful, event-driven, message-passing
substrate computes an `NC1`-complete function with `O(1)` spatial resources and
`Theta(n)` temporal depth, checked against a sequential oracle over hundreds of runs with
nondeterministic interleavings. The "temporal state, not spatial parallelism"
claim is now executable rather than asserted.

**Does not discharge: the complexity witness standing alone.** The complexity
content is Barrington's theorem plus the resource measurement. The theorem is
imported, not established here.

**Does not discharge: composition.** Untouched. The third leg of the conjunction
gate remains open.

**Says nothing about selection.** This network was hand-constructed. Every
weight, delay and threshold was chosen. It is a statement about what the
substrate can *represent*, and no statement whatever about what any training
procedure would *find*. That is the expressibility/selection gap in its usual
place, and this witness sits entirely on the expressibility side of it.

The honest one-line summary: the cage can be escaped on this substrate, at
constant hardware cost, and we can now test whether a specific implementation
does it correctly. Whether anything could learn to do it remains untouched.

## 8. Reproducing

```bash
go build -o damned .

# encoding validation only, no distributed runs
./damned -scenario=word -group=s5 -len=32 -words=5 -reps=0

# the witness
./damned -scenario=word -group=s5   -len=32 -words=5 -reps=20 -procs=3 -guard=2009L
./damned -scenario=word -group=z120 -len=32 -words=5 -reps=20 -procs=3 -guard=2009L

# the defect
./damned -scenario=fanin-local -procs=2 -guard=2009  -reps 600   # fails
./damned -scenario=fanin-local -procs=2 -guard=2009L -reps 600   # clean

# regression
./damned -scenario=cancel -guard=2009L -reps 300
./damned -scenario=ring   -guard=2009L -reps 300
```

New flags: `-group` (`s5|z120|s3|s4|z6|z24`), `-len`, `-words`, `-procs`.
New guard value: `2009L`. New scenarios: `word`, `fanin`, `fanin-local`.

## 9. Next

1. ~~Answer the TLA+ question~~ Answered 2026-11-24: the model cannot detect
   this class (section 7 of the defect report). Extending it with weights and a
   spike-train exactness invariant is the open item.
2. **Sweep `dmin`.** The construction fixes all delays at 1, so `dmin=1` and
   lookahead is minimal. Parameterising the inter-layer delay turns this into a
   direct measurement of serialization cost against lookahead — the crossover
   quantity, on a substrate that actually needs it.
3. **`A5` versus `S4`.** A second control pair, non-solvable against solvable at
   *unmatched* order, to confirm the resource profile tracks `|G|·|S|` and
   nothing else.
4. **Composition witness.** Still open, and now the binding constraint on the
   Tier 1 conjunction.
