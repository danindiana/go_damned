# Verifying the 2009L guard

*2026-11-24.* Follow-up to [`DEFECT-local-straggler.md`](DEFECT-local-straggler.md). Diagrams: [`diagrams/2026-11-24-verification/`](../diagrams/2026-11-24-verification/).

The defect report showed the `2009` guard, as reconstructed in `Damned.go`, committing spikes the sequential simulation does not produce, and proposed `2009L`. Its evidence was a handful of hand-built scenarios run a few hundred times each. This note adds three checks and one fix:

1. a reading of the 2009 paper, to see whether the defect is the paper's or the reconstruction's;
2. a random-network search in Go (`fuzz_test.go`);
3. a new TLA+ model with real neurons, model-checked over every interleaving (`formal-verification/weighted/`);
4. a fix to the event order at equal times, which the long random search showed was under-specified (section 2.1).

## Summary

| Check | `2009` | `2009L` |
|---|---|---|
| Hand-built scenarios (`go test`) | fails `fanin-local`, the S5 witness | 0 wrong runs |
| Random networks, Go engine (8,000 networks, 15 runs each) | 39 of 8,000 diverged | 0 of 8,000 (after the equal-time fix, section 2.1) |
| TLA+, five named networks, every interleaving | wrong spike in 3 of 5 | no violation, no deadlock in 5 of 5 |
| TLA+, random networks, every interleaving | wrong spike in 1 of 120 | no violation, no deadlock in 120 of 120 |
| TLA+, the paper's rule in three readings (section 1.1) | as printed (`≤`): wrong spikes, even from remote inhibitors | only with own clock and strict `<`: no violation (= `2009L` one at a time) |

Read the `2009L` column at its actual strength. The Go search samples interleavings. TLC covers every interleaving, but only of the specific small networks it was given, under the simplifications listed below. Neither is a proof for all networks.

## 1. The 2009 paper

Source: A. Mouraud and D. Puzenat, *Simulation of Large Spiking Neural Networks on Distributed Architectures, The "DAMNED" Simulator*, EANN 2009, CCIS 43, pp. 359–370 ([doi](https://doi.org/10.1007/978-3-642-03969-0_33); open copy at LIRIS, `Liris-4293.pdf`). Read through a text extraction of the PDF, which loses some symbols; the points below should be confirmed against the printed page.

What the paper says:

- **Processing check (Algorithm 3).** An event with date `de` may be processed if `tp_i = de`, or if for the other tasks `clock_sent[j]` holds and `de ≤ |te_j| + δmin`, or if `te_j = 0`. The index range is printed as `0 < j < P`, with no `j ≠ i`; Algorithm 2 prints `0 ≤ j ≤ P − 1` with an `i`/`j` condition whose symbol is lost in extraction.
- **Equal times.** "All inhibitory events for a given time increment has to be computed before excitatory ones to avoid the emission of erroneous events." The sequential reference in `Damned.go` uses the same rule.
- **Firing.** A neuron decides to spike when it processes an incoming event; spike-time prediction is mentioned only as an option with an a-posteriori validity flag.
- **Processing.** "CPC processes the top event of the EtoP queue": one event at a time.

What that implies:

- **Neither reading of the index range closes the hole.** If `j` excludes the processor itself, the check has no local term at all, which is the reconstruction's `2009`. If it includes the processor, the local term is `de ≤ |te_i| + δmin`. In `fanin-local` the local inhibitor's spike sits at `te_i = 1` with `δmin = 1`, the target's event has `de = 2`, and `2 ≤ 1 + 1` admits it, although the inhibitory impact lands at exactly `t = 2` and must be applied first. The `≤` admits the boundary case that `2009L` excludes with a strict `<`.
- **The rebuild's concurrency is not the cause.** `Damned.go` lets several computations run at once (one goroutine per neuron), while the paper describes one at a time. The TLA+ model has a switch for this (`SERIAL`). With one computation at a time, `2009` still commits a wrong spike in all three networks where it failed before, and `2009L` still has none (section 3). In the one-at-a-time trace for `fuzz_min`, N1's spike at t = 0 is forecast but not yet emitted when processor 2 integrates N2's stimulus at t = 2; N2 fires; N1's −4 arrives afterwards, too late.

**Conclusion from the text alone.** The paper's processing rule, read literally, does not prevent this race at the boundary, but its notation for `te` and `de` is not tight enough to settle the question by reading. Section 1.1 settles as much as can be settled by model-checking three readings.

### 1.1 Model-checking the paper's rule (roadmap 1.2)

*Added later on 2026-11-24.* The full text is paywalled at Springer, and the open LIRIS copy could only be read through a text extraction. That extraction gives Algorithm 3 line by line:

```
1 if (tpi = de) or
2 (clock sent[j] and (de ≤ |tej | + δmin), 0 <j<P) or (tej = 0)) then
  tpi ←− de
3 if (tei = 0) then
  tei ←− −tpi
  → processing granted
else
4 tpi ←− −de
  → processing denied
```

Two things are not settled by the text: whether `j` ranges over the task itself, and whether the paper's clocks make `≤` safe. The paper's EtoP events carry the spike's *emission* date, while the model and `Damned.go` stamp events with their *impact* time, so an off-by-one between the two conventions cannot be ruled out. Rather than guess, the TLA+ model now has the processing rule in each reading as a guard (`formal-verification/weighted/DamnedWeighted.tla.tmpl`). The peer bound `kb[p][m]` (the earliest stamp at which `m`'s future spikes can land) plays the part of `|te_j| + δmin`, and this processor's earliest forecast spike plays `te_i`.

| Guard | Other tasks | The task's own forecasts (`te_i`) | Reading |
|---|---|---|---|
| `2009P1` | stamp ≤ their bound | not checked | A, as printed |
| `2009P2` | stamp ≤ their bound | stamp ≤ earliest forecast + δmin | B, as printed |
| `2009P3` | stamp < their bound | stamp < earliest forecast + δmin | B, with strict `<` |
| `2009` (reconstruction) | stamp < their bound | not checked | A, with strict `<` |

The emission rule is left as in the model (`localClear` plus a strict peer check), which is stricter than the paper's Algorithm 2, so any failure below comes from the processing rule. The `tp_i = de` and `te_j = 0` shortcuts are not modelled. Results on the named networks ("one at a time" is the paper's single CPC):

| Network | `2009P1` | `2009P2` | `2009P3`, concurrent | `2009P3`, one at a time |
|---|---|---|---|---|
| `cancel` (inhibitor on another processor) | **wrong spike** | **wrong spike** | no violation | no violation |
| `fanin_local` | **wrong spike** | **wrong spike** | **wrong spike** | no violation |
| `fuzz_min` | **wrong spike** | **wrong spike** | **wrong spike** | no violation |
| `mixed` | **wrong spike** | **wrong spike** | **wrong spike** | no violation |
| `equal_stamp` (no inhibition) | no violation | no violation | no violation | no violation |

`2009P1` and `2009P2` fail with one computation at a time as well. `2009P3` fails only when several computations run at once, because it ignores computations still running; `2009L` adds exactly that.

Over the 120 random networks of section 3 (the same networks, regenerated from the same seed), one computation at a time, as in the paper:

| Guard | No violation | Wrong spike |
|---|---|---|
| `2009P1` (reading A, `≤`) | 114 | **6** |
| `2009P2` (reading B, `≤`) | 116 | **4** |
| `2009P3` (reading B, `<`) | 120 | 0 |
| `2009` (reconstruction), for comparison | 119 | **1** |
| `2009L`, for comparison | 120 | 0 |

No run deadlocked or was cut short. One line per network: [`../formal-verification/weighted/random_paper/summary.txt`](../formal-verification/weighted/random_paper/summary.txt).

**What this settles.** The paper's processing rule is correct, in this model, only under one reading: the task's own clock is included in the check (reading B), *and* the paper's clock convention makes its printed `≤` behave as a strict `<`. That reading is `2009P3`, which with one computation at a time is the same rule as `2009L`. Under any other reading the rule lets late spikes through: with `≤` taken at face value, even spikes from another processor (the `cancel` network); with the task's own clock left out, the local late spikes this report is about. Whether the published algorithm is correct therefore turns on two details of notation the extracted text cannot show: the `j` range and the `te` convention. The printed page, or the authors, can answer both.

## 2. Random-network search (Go)

`fuzz_test.go` builds random networks: 3–6 neurons on 2–4 processors, thresholds 1–3, signed weights, delays 1–3, 1–3 stimuli. Each runs repeatedly under the guard being tested, at zero latency, and is compared spike for spike with `sequentialEDS`. A failing case is shrunk by removing synapses and stimuli while it still fails.

The shrinker found a two-neuron counterexample for `2009`, smaller than `fanin-local`:

```
P = 2; N1 and N2 both on processor 2, threshold 2
N1 -> N2: weight -4, delay 2
stimuli: N1 at t = 0, N2 at t = 2 (each +2)
correct: N1 fires at 0; at t = 2 the -4 comes first, so N2 never fires
2009:    processor 2 integrates N2's stimulus before N1's spike is emitted; N2 fires
```

Results:

Default run (400 random networks per guard, 10 runs each, seed 1): `2009` diverged on 3, `2009L` on 0.

### 2.1 Equal-time order: a gap in the specification, now closed

The first long run (8,000 networks, 15 runs each, seed 11) found **5 networks where `2009L` diverged**. The shrunk case was not a timing race:

```
P = 3; neurons N0..N4, thresholds 2,1,3,1,2 on processors 1,3,2,3,2
N4 -> N0 (+2, delay 3);  N0 -> N2 (+2, delay 2);  N2 -> N0 (+1, delay 3)
stimuli: N0 at t = 2, N2 at t = 0, N4 at t = 0
correct: [0 2] [0 4] [2 0] [3 0] [5 2]
2009L:   the same, plus [8 0]
```

At t = 3, N0 receives +1 from N2 and +2 from N4. A neuron resets to 0 when it fires, so the order matters. With +1 then +2, N0 fires on reaching 3 and keeps 0. With +2 then +1, it fires on reaching 2 and keeps the leftover +1, so the +1 that arrives at t = 8 makes it fire again. The sequential reference took the order in which it had created the impacts; the engine took the order in which they arrived. The 2009 paper says inhibitory impacts come before excitatory ones and says nothing about order within a sign, so neither was wrong: the rule was missing.

**Fix.** Both `sequentialEDS` and the engine's queue (`cmQueue.pop`) now order equal-time events by (stamp, inhibitory first, source neuron), with stimuli first. The engine can always honour this under `2009L`, because the guard admits an event at stamp `s` only once nothing at or before `s` can still arrive, from peers or from local forecasts. Every impact at `s` is therefore already queued when the first one is integrated. Under `2009` that is not guaranteed, which is the defect itself. The shrunk case is now `TestEqualStampOrder2009L` (500 of 500 runs correct), and the TLA+ model and its generator use the same order.

The same long run, repeated after the fix:

| Guard | Networks | Runs each | Diverged |
|---|---|---|---|
| `2009` | 8,000 | 15 | **39** |
| `2009L` | 8,000 | 15 | **0** |

Same seed (11) and networks for both.

### 2.2 Classifying every 2009 failure

To check whether every `2009` failure has the local-inhibitor shape, `TestFuzzClassify2009` reran the same 8,000 networks under `2009`, shrank every failing one, and recorded which synapses the shrunk case still needs. A synapse is *local* if its source and target neurons are on the same processor. This run found 41 failures (39 in the run above; the count depends on scheduling). Full output: [`results/2026-11-24-classify-2009.txt`](results/2026-11-24-classify-2009.txt).

| Synapses the shrunk failure still needs | Failures |
|---|---|
| A local inhibitory synapse (and nothing local and excitatory) | 31 |
| A local inhibitory and a local excitatory synapse | 8 |
| A local excitatory synapse only, no inhibition at all | **2** |
| Remote synapses only | 0 |

`2009L` was correct on all 41 shrunk cases (300 runs each).

**The defect is broader than a local inhibitor.** Every failure needs a *local* synapse, and none is explained by remote traffic alone, which is what the `2009` peer check covers. But two need no inhibition. Case 5, shrunk:

```
P = 3; N1 (threshold 3) on processor 2
N4 -> N1: +2, delay 2, LOCAL (N4 also on processor 2)
N2 -> N1: +2, delay 2, remote (N2 on processor 1)
stimuli: N4 at t = 0, N2 at t = 1
correct: N1 gets +2 at t = 2 and +2 at t = 3, fires at t = 3
2009:    N4's spike is forecast but not yet emitted when processor 2 integrates
         the remote +2 (stamp 3); the local +2 (stamp 2) arrives afterwards and
         N1 fires stamped t = 2: right neuron, wrong time
```

The other (case 15) ends with a neuron firing twice at the same stamp. Both are the same race as before: an impact from a spike forecast on the same processor arrives after a later-stamped event has been integrated. When the late impact is inhibitory the result is an extra spike; when it is excitatory, a spike at the wrong time or a duplicate. `2009L` blocks the integration in every case, because its local bound is taken over every forecast and running computation, whatever its sign.

Run it yourself:

```bash
go test -run TestFuzz -v .                                   # 400 networks per guard, ~1 minute
FUZZ_CASES=8000 FUZZ_REPS=15 FUZZ_SEED=11 go test -run TestFuzz -v -timeout 4h .   # ~35 minutes
```

## 3. TLA+ model with real neurons

`formal-verification/weighted/DamnedWeighted.tla.tmpl` models the `Damned.go` engine with what the earlier model lacked:

- integer potentials, thresholds and signed weights;
- the engine's event order (time, then inhibitory first, then source neuron), and a neuron that is still computing blocks the queue, as in `tryComputeAndEmit`;
- forecast spikes waiting for emission, FIFO channels between each ordered pair of processors, and bounds computed as in `myBound` and overwritten on receipt;
- the computation gate for `2009` (peer bounds) and `2009L` (peer bounds plus the local bound);
- invariants that compare against the **correct spike train**: `NoWrongSpike` (nothing committed outside it) and `CompleteAtEnd` (all of it committed once everything drains), plus TLC's deadlock check.

`gen.py` writes one spec per network, with the correct spike train computed by the same rules as `sequentialEDS`. `run_tlc.sh` checks each under both guards.

Simplifications:

- Stimuli are in their owners' queues from the start; the environment process sends nothing else and bounds nobody.
- Each spec is one fixed network. TLC covers every interleaving of that network, not every network.
- Channels hold at most 3 messages; bounds at or above `TMAX + 1` are treated as infinite, which is exact here because later impacts are dropped.
- Liveness (that runs always finish) is not checked beyond the absence of deadlock.

### Named networks

Distinct states in brackets. "Concurrent" lets each processor compute for several neurons at once, as `Damned.go` does; "one at a time" is the paper's single CPC (`SERIAL = TRUE`).

| Network | `2009`, concurrent | `2009`, one at a time | `2009L`, concurrent | `2009L`, one at a time |
|---|---|---|---|---|
| `cancel` (inhibitor remote) | no violation (133) | no violation (129) | no violation (127) | no violation (127) |
| `fanin_local` (inhibitor local) | **wrong spike** | **wrong spike** | no violation (195) | no violation (190) |
| `fuzz_min` (two neurons) | **wrong spike** | **wrong spike** | no violation (48) | no violation (48) |
| `mixed` (local and remote inhibitors) | **wrong spike** | **wrong spike** | no violation (340) | no violation (297) |
| `equal_stamp` (section 2.1; no inhibition) | no violation (814,001) | no violation (813,748) | no violation (814,001) | no violation (813,748) |

No run reached a deadlock.

The `fanin_local` counterexample TLC found: processor 1 is still computing the stimulus for the local inhibitor (stamp 1) when it starts, finishes and emits the target's events at stamp 2. The `2009` gate checks only the peer's bound, which allows stamp 2. The target fires and commits `<<2, 1>>`, which is not in the correct spike train. `2009L` blocks the stamp-2 computation because `2 < 1 + DMIN` fails.

### Random networks

120 random networks from `gen.py --random 120 2026`: 2 or 3 processors (34 have 3), 2 to 4 neurons, signed weights, delays 1 or 2, 1 to 3 stimuli, `TMAX = 4`. Networks in which nothing fires were skipped. Each was checked four ways. One line per network and variant is in [`../formal-verification/weighted/random/summary.txt`](../formal-verification/weighted/random/summary.txt).

| Variant | No violation | Wrong spike | Deadlock or incomplete |
|---|---|---|---|
| `2009`, concurrent | 119 | **1** | 0 |
| `2009`, one at a time | 119 | **1** | 0 |
| `2009L`, concurrent | 120 | 0 | 0 |
| `2009L`, one at a time | 120 | 0 | 0 |

The one `2009` failure (`r061`) is again a local inhibitor: both neurons on processor 2, an inhibitory synapse from N2 to N1, and a stimulus to N1 at the stamp the inhibition lands. State spaces ranged from 60 to 1,054,337 distinct states (median 138).

Reproduce:

```bash
cd formal-verification/weighted
python3 gen.py                      # named networks
python3 gen.py --random 120 2026    # random networks -> random/
./run_tlc.sh /path/to/tla2tools.jar .
./run_tlc.sh /path/to/tla2tools.jar random
```

## What this establishes, and what it does not

- **Established:** the `2009` computation gate, as reconstructed, admits wrong spikes whenever a spike forecast on the target's own processor delivers its impact after a later-stamped event has been integrated (usually through a local inhibitory synapse, sometimes an excitatory one), and TLC exhibits that on a model independent of the Go code, with or without concurrent computation. With the equal-time order fixed, `2009L` removed every failure found by every method here.
- **Not established:** that `2009L` is correct for all networks, all channel capacities, or with the environment process sending stimuli over time; or that the published algorithm has the same defect.

## Next steps

These are tracked, with the rest of the project's next steps, in the [README roadmap](../README.md#roadmap).

1. ~~Implement the 2009 paper's rule as a fourth guard~~ Done for Algorithm 3 in the TLA+ model, in three readings (section 1.1). Still open: confirm the `j` range and the `te` convention against the printed page, and model Algorithm 2's shortcuts.
2. Prove the `2009L` gate sound for all networks, rather than checking instances: either an inductive invariant checked with TLAPS or Apalache, or a paper proof of the form "no impact can arrive below the gate's horizon".
3. Model the environment process sending stimuli over time, and check with larger channel capacities.
