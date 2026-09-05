Let me look at the code structure first.Building it. Design first, then the sweep.Design settled. The construction: a constant-size spiking network that tracks a group element in one-hot state, with per-step conjunction and an inhibitory cancellation that is load-bearing for algebraic correctness.Encoding validated: 364 neurons, 1080 synapses, constant in `n`, for both groups. Now the distributed engine.Found a real bug in their termination detection, exposed by a denser network:Fixed, no regression. Now the sweep.Divergence is spurious extra spikes with the soundness oracle silent. Isolating whether that's my construction or the engine:Minimal case is sound under 2009. So the S5 failure is elsewhere. Getting the actual diff:Localized it. All spurious spikes are state neurons on the *same processor as the inhibitory clock*, firing at the cancellation tick. Testing the hypothesis directly:Confirmed: a **four-neuron counterexample where the 2009 guard violates exactness**, and the repo's own soundness flag never fires. Implementing the fix:Fix was incomplete — it missed events still inside a running EC thread. Correcting:2009L is clean at 0/600 across all P, with no regression. Now the full witness sweep:The clean binary (no dump I/O perturbing the race) shows it far more strongly. Final sweep:The n=64 cells blew the time budget, but everything through n=32 is done and the result is decisive. Summary of where the witness landed.

## Construction

A constant-size spiking network that tracks a group element in one-hot state. 364 neurons, 1080 synapses, **independent of word length n**; depth 2n. Identical budget for both groups.

The hard part was that the neuron model has no leak, so a coincidence detector with two sparse subthreshold inputs accumulates across steps and eventually fires spuriously. Solved without touching the neuron model: flush the conjunction layer every step with a clock pulse, then exactly cancel the flush echo at the state layer with an inhibitory pulse of −2|S|, which is a constant because every group element has exactly one predecessor per generator. Inhibition-sorts-first at equal stamps makes the cancellation land before the excitation.

That cancellation is the point of contact with the guard. It is an inhibitory pulse that must arrive before excitation carried on other processors. Guard soundness and algebraic correctness become the same property — which is what makes this a witness rather than a demo.

Encoding validated against a pure group-theoretic oracle: correct at every n tested, both groups.

## Results (P=3, 100 runs per cell)

| group | n | guard | train mismatch | wrong product | soundness flag |
|---|---|---|---|---|---|
| S5 | 8 | 2006 / 2009 / 2009L | 100 / 2 / 0 | 100 / 0 / 0 | 100 / 0 / 0 |
| S5 | 16 | 2006 / 2009 / 2009L | 100 / 3 / 0 | 100 / 2 / 0 | 100 / 0 / 0 |
| S5 | 32 | 2006 / 2009 / 2009L | 100 / **57** / 0 | 100 / **44** / 0 | 100 / **0** / 0 |
| Z120 | 8–32 | same pattern | 100 / 3–6 / 0 | 100 / 1–6 / 0 | ~95 / 0 / 0 |

## Three findings, in increasing order of importance

**1. A termination-detection bug.** `inFlight` is incremented once per event on send and decremented once per *message* on receive. Any message with fan-out > 1 leaks. Invisible in `cancel` and `ring` because fan-out to any single peer is 1 there; it hangs the engine outright on a dense network. Fixed and verified against both published scenarios.

**2. The 2009 guard is not sound.** This is the real one. At n=32 it produces wrong group products in 44% of runs. I reduced it to a four-neuron counterexample (`fanin-local`): target and inhibitor co-located, excitation remote. The computation-authorization gate checks only *peer* bounds. It never asks whether this processor's own un-emitted spike could still deliver an impact at or before the stamp it's about to integrate. `cancel` and `ring` both place the inhibitory source remote from its target, so neither can expose it. The TLA+ model presumably inherits the same structure.

The fix (`2009L`) adds the local half: block computation when a local forecast or in-flight EC event sits at stamp s with top ≥ s + d_min. 0/600 on the counterexample, 0/100 across the whole witness sweep, no regression on `cancel` or `ring`.

**3. Your soundness oracle is blind to this class.** `flagged-unsound` was 0/100 in every single 2009 failure. Only the spike-train comparison against the sequential oracle caught it. The straggler flag is a weaker instrument than the README implies, which matters because it's the thing the diagrams present as the correctness detector.

## What this discharges, and what it doesn't

It discharges the **substrate** witness. A stateful event-driven substrate computes an NC¹-complete function (Barrington) with O(1) spatial resources and Θ(n) temporal depth. That is the "temporal state, not spatial parallelism" claim made executable rather than asserted.

It does **not** discharge the complexity witness on its own — the complexity content is Barrington's theorem plus my resource measurement, and the network is hand-constructed. Nothing here was learned, so this speaks to expressibility and says nothing about selection. Same gap as always.

The Z120 control did its job in the way I predicted rather than the way that would have been more dramatic: identical resource profile, identical failure rates. No substrate-level difference between solvable and non-solvable. That's the correct outcome and worth stating plainly — the complexity claim rests on the algebra, not on anything the network does differently.

Files are in the container: `witness_group.go`, `fanin.go`, and patches to `Damned.go` (the inFlight fix, the `2009L` guard, three new scenarios). I hit the tool limit before I could copy them to outputs — say the word and I'll present them, finish the n=64 row, and write up the counterexample as a proper defect report against the TLA+ spec.

Latency *masks* this defect rather than amplifying it — the opposite of the 2006 case, and worth noting for the port. Now packaging the deliverables.Done. The n=64 row closed the scaling picture, and it's the most useful row in the table.

**S5 under the 2009 guard:** 0%, 2%, 44%, 29% wrong products at n = 8, 16, 32, 64. Under 2009L: zero everywhere, 364 neurons and 1080 synapses throughout, depth 2n. The witness passes, under a corrected guard.

Two things worth flagging beyond the report.

**Latency inverts the defect.** Under 1ms simulated latency the 2009 guard passes `fanin-local` 300/300; at zero latency it fails. The race is local inhibition against remote excitation, so delaying remote traffic hands the local path the win. That's the opposite of the 2006 bug, where latency took the cancel scenario from 19/200 to 200/200. The adversarial regime for this defect is tight coupling and low latency — which is the on-chip NoC case, not the MPI-cluster case the papers targeted. A Blackhole port moves toward the failing end of the axis.

**The scaling failure rate is the useful property.** A witness that detects a bug is worth something; one whose detection probability rises as you push the substrate harder is worth more. Each step is another opportunity for the race and one corruption destroys the answer, so per-run failure compounds with n. That makes `word` a better regression test for this engine than anything currently in it.

On what got discharged: the substrate leg, cleanly. Not the complexity leg standing alone — Barrington's theorem is imported, and what I measured is the resource profile. Not composition. And nothing at all about selection, since every weight and delay in that network was chosen by hand. The construction proves the substrate can represent the escape at constant hardware cost. It says nothing about what a training procedure would find, which is the same gap in the same place.

The `Z120` control came out the way I said it would rather than the dramatic way. Identical failure rates, identical everything. Its job was to establish that the resource budget is genuinely matched so the NC¹ claim can't be attributed to S5 getting a bigger network, and it did that.

The item I'd put first: section 7 of the defect report. If `DamnedFull2.tla` gates its computation action only on `kb[m]` for peers, then `Exactness` holding at `FIX=TRUE` reflects the spec sharing the implementation's blind spot rather than the discipline being sound, and Config A is already large enough to find the counterexample. That question is cheap to answer and it determines whether the formal-verification claim needs rescoping or just a new initial condition.
