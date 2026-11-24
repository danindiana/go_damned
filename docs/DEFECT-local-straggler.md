# Defect: the 2009 guard omits the local half of the CMB discipline

**Severity:** exactness violation. The guard the repository presents as correct,
and that TLC reports as satisfying `Exactness`, commits spikes that the
sequential oracle does not produce.

**Status:** minimal counterexample below, four neurons. Fix implemented as
guard `2009L`. In every run so far, `2009L` gives 0 failures where `2009` fails
(up to 57% of runs at the word-problem witness's n = 32 on 2026-09-05; up to 58%
on this counterexample on 2026-11-24). These are tests over sampled
interleavings, not a proof that `2009L` is sound.

**Scope:** this is the `2009` guard as reconstructed in `Damned.go` from the
2009 EANN paper (see the README's references). A reading of the paper's
processing rule (Algorithm 3) and a model of one-at-a-time processing, as the
paper describes, are in [`VERIFICATION-2009L.md`](VERIFICATION-2009L.md): the
race survives one-at-a-time processing, and the paper's rule as printed admits
the boundary case, but its notation is not tight enough to settle the question
from the text alone.

**Reported:** 2026-09-05. **Rerun and integrated into the main tree:** 2026-11-24.

---

## 1. Statement

`tryComputeAndEmit` gates the start of a computation on peer bounds only:

```go
top := pl.cm.minStamp()
ok := true
if e.cfg.Guard == "2009" {
    for _, b := range pl.clk {      // pl.clk holds PEER bounds
        if !(top < b) { ok = false; break }
    }
}
```

`pl.clk` is indexed by peer. It does not describe this processor. So the gate
proves that no *remote* processor can still deliver an impact at or before
`top`, and proves nothing about work this processor is itself holding.

But this processor may hold, at that instant:

- a `CPEvent` in `pl.cp`: a neuron already known to fire at stamp `s`, whose
  emission is authorized separately and has not yet happened; or
- a `CMEvent` in `pl.running`: an impact inside an EC goroutine which may yet
  cross threshold and produce a spike at stamp `s`.

Either becomes an impact on a local target at `s + d`, `d >= dmin`. If
`top >= s + dmin` and that impact is inhibitory, the sequential oracle applies
it *before* the event at `top` — inhibition sorts first at equal stamps — and
the target does not fire. The distributed engine integrates the excitation
first and commits a spike that has no counterpart in ground truth.

The emission guard does check `localClear`, but that is the wrong place and the
wrong quantity: it runs at commit time on events already in `pl.cm`, and by then
the premature computation has happened.

## 2. Why the existing scenarios cannot expose it

| scenario | inhibitory source | its target | exposes? |
|---|---|---|---|
| `cancel` | N3 on proc 2 | N0 on proc 1 | no — remote |
| `ring`   | N0 on proc 1 | N3 on proc 1 | no — no competing remote excitation at the same stamp |

Both place the inhibition where the *peer-bound* half of the guard covers it.
The hole is on the complementary path: inhibition **local** to its target,
excitation **remote**. No test in the repository has that shape.

This is worth stating plainly because it is a coverage failure, not bad luck.
The two scenarios were built to exercise the 2006 bug, and they do that well.
They were not built to search the space of source-placement configurations, so
the second configuration was never tried.

## 3. Minimal counterexample — `scenario=fanin-local`

Four neurons, the mirror image of `cancel`:

```
N0  theta=2   proc 1     target
N1  theta=1   proc 2     excitatory, REMOTE, w=+2 d=2  -> impacts N0 at t=2
N2  theta=1   proc P     excitatory, REMOTE, w=+2 d=2  -> impacts N0 at t=2
N3  theta=1   proc 1     inhibitory, LOCAL,  w=-4 d=1  -> impacts N0 at t=2
stim: N1@0, N2@0, N3@1
```

Ground truth: `[[0 1] [0 2] [1 3]]`. N0 never fires — at t=2 the -4 sorts first,
so the potential path is 0 -> -4 -> -2 -> 0.

Observed, 600 runs, latency 0:

| P | guard 2006 | guard 2009 | guard 2009L |
|---|---|---|---|
| 2 | 23/600 | **17/600** | 0/600 |
| 3 | 47/600 | 0/600 | 0/600 |
| 4 | 116/600 | **6/600** | 0/600 |

`flagged-unsound` is **0** in every 2009 failure. See section 5.

Rerun on 2026-11-24 (same build of the patch, 600 runs per cell, latency 0):

| P | guard 2006 | guard 2009 | guard 2009L |
|---|---|---|---|
| 2 | 12/600 | **348/600** | 0/600 |
| 3 | 79/600 | 2/600 | 0/600 |
| 4 | 206/600 | 2/600 | 0/600 |

The `2009` failure rate is not stable between runs or machines (17/600 then
348/600 at P = 2), so quote it as "fails", not as a rate. The `2009L` result
has been 0 in every run.

## 4. Fix

Add the local half of the same bound computation. A local forecast or in-flight
computation at stamp `s` can produce impacts no earlier than `s + dmin`, so no
event at or after that may be integrated yet:

```go
if ok && e.cfg.Guard == "2009L" {
    selfB := INF
    for _, c := range pl.cp      { if c.St < selfB { selfB = c.St } }
    for _, r := range pl.running { if r.St < selfB { selfB = r.St } }
    if selfB < INF && !(top < selfB+e.dmin) { ok = false }
}
```

`pl.cm` is deliberately excluded: its minimum *is* `top`, and including it would
self-block. The two collections that matter are the ones `myBound` already
consults but the computation gate does not.

**No deadlock.** Emission of the blocking CP event `c` requires
`localClear(c.Ni, c.St)`, which may require computing a local event at stamp
`<= c.St = selfB < selfB + dmin`. Such an event is still authorized, so the
blocking event always drains.

**No regression.** `cancel` and `ring` are 0/300 under both `2009` and `2009L`.

## 5. Second defect: the soundness oracle does not detect this class

`flagged-unsound` was **0** in every single 2009 failure recorded here — 0/600
on `fanin-local`, 0/100 on every cell of the word-problem sweep — while the
spike-train comparison against `sequentialEDS` caught all of them. On `fanin`
with the 2006 guard the oracle reports 0/300 against 275/300 actual mismatches.

`PendingWouldCancel` is therefore a considerably weaker instrument than the
README and the proofs diagrams present it as. Anything relying on it as *the*
correctness detector — rather than on oracle comparison — is under-testing.
Recommend demoting it in the documentation to a diagnostic hint and promoting
spike-train equality to the stated criterion.

## 6. Third defect (unrelated, found first): inFlight accounting leak

`processorLoop` decrements `inFlight` once per **message**; the send side
increments once per **event**. Every message with fan-out > 1 leaks
`len(Evs)-1`, quiescence is never detected, and the engine hangs. Invisible in
`cancel`/`ring` because per-peer fan-out is 1 in both. Fix is one line, included
in the patch.

## 7. Consequences for the TLA+ model

*Answered 2026-11-24 by reading `formal-verification/DamnedFull2.tla`; TLC was
not re-run.*

The model cannot detect this defect, for three independent reasons:

1. **Same gate.** `CompAuth(p, e)` under `FIX = TRUE` is
   `\A m \in All \ {p} : e.st < kb[p][m]`, peer bounds only. It has the same
   blind spot as the Go `2009` guard.
2. **Same detector.** `Exactness == ~bad`, and `bad` is set at the commit point
   from `PendingFor(e.ni, e.st)`: is any impact on the neuron still pending at
   or before its stamp? That is the predicate the Go soundness flag uses, which
   reports 0 in every failure of this class (section 5). The emission guard's
   local check (`LocalClear` in the model, `localClear` in Go) holds the
   premature spike until the inhibitory impact has been integrated. By the
   commit point nothing is pending, so `bad` stays false even though the
   integration order was wrong.
3. **No cancellation semantics.** The model has no synaptic weights and no
   membrane potential: `FinishComp` fires or stays silent nondeterministically,
   and `Post(n)` carries only targets and delays. Integrating events in the
   wrong order changes nothing the model can observe.

So TLC's clean result for the `2009` discipline (Config A exhaustive, 740,431
states; Config B bounded) says the commit-time pending check never fires in the
reachable states at those constants. It does not say the spike train matches
the sequential simulation, and it does not cover the local-straggler case.

To make the model able to see this class, it needs integer potentials with
signed weights, the inhibition-first ordering at equal stamps, and an
exactness invariant that compares the committed spike train against a
sequential reference rather than checking for pending events.

## 8. Note for the on-chip port

Message latency **masks** this defect rather than amplifying it — the opposite
of the 2006 bug's behaviour. Under 1ms simulated latency the 2009 guard passes
`fanin-local` 300/300; at zero latency it fails. The race is local inhibition
against remote excitation, so delaying remote traffic lets the local path win.

The adversarial regime is therefore tight coupling and low latency, which is
precisely the on-chip NoC case rather than the MPI-cluster case the papers
targeted. A port to a mesh interconnect moves toward the failing end of this
axis, not away from it.
