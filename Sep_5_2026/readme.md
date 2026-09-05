This is a real literature question spanning several fields that don't cite each other much. Let me search properly.Six clusters, organized by where each would sit in your loop rather than by field, since these literatures barely cite each other.

## 1. The loop skeleton — and why yours is unusually ready for it

FunSearch and AlphaEvolve are the reference architecture: an LLM proposing program mutations, an automatic evaluator scoring them, an archive doing the explore/exploit. AlphaEvolve orchestrates a pipeline of LLMs that make direct changes to code, taking continuous feedback from one or more evaluators, and the evaluation mechanism is what lets it discard the base model's incorrect suggestions. The archive is MAP-Elites-inspired; later systems add adaptive parent sampling, novelty rejection, and budget control.

The observation that matters for you: the near-term constraint on this architecture is not the LLM, it is the evaluator — problems where correctness is expensive to verify, or where "better" is subtle and contextual, do not fit.

You spent this session building exactly the thing that's usually missing. `sequentialEDS` + `wordProduct` + spike-train equality is a cheap, exact, fully automatic correctness oracle, with a difficulty knob (`n`) that makes detection *easier* as you push, and a matched control. That is a rarer asset than the search machinery, which is off-the-shelf.

- AlphaEvolve white paper: arXiv 2506.13131
- FunSearch: Romera-Paredes et al., Nature 2024
- Open-source: ShinkaEvolve, CodeEvolve (arXiv 2510.14150), OpenEvolve

## 2. The mapping problem — biology onto target hardware

Directly your Blackhole question, and it's a mature subfield nobody in the complexity literature reads.

SpiNeMap splits it into SpiNeCluster (heuristic partitioning to minimise spikes on global synapses) and SpiNePlacer (meta-heuristic placement to minimise energy and latency). NeuMap does the same with congestion-aware placement minimising hop distance. The most on-point recent one treats partitioning and placement on Loihi 2 as a black-box mapping problem solved by hardware-guided evolutionary search, noting that prior neuromorphic work focused on performance modelling rather than automated mapping.

For the hardware-agnostic angle: UH-NAS uses LLMs as evolutionary operators and exposes hardware as a swappable backend with per-platform energy models, physical constraints, and non-ideality simulators, so the same search algorithm compares across backends. That backend-swap pattern is the right shape for "map onto Blackhole *and* Loihi *and* SpiNNaker and see which the substrate prefers."

- SpiNeMap: arXiv 1909.01843 · Loihi 2 evolutionary mapping: arXiv 2602.04717 · UH-NAS: arXiv 2606.10294
- Survey: SNN Architecture Search, arXiv 2510.14235
- The floorline work is worth reading for its bottleneck taxonomy: memory-bound, compute-bound, traffic-bound

## 3. Discovering the formalism rather than hand-writing it

This is the closest existing analogue to what you'd want: don't hand-design the plasticity rule, search for it under functional constraints. Confavreux et al. parameterise plasticity rules by a Volterra expansion and use gradient descent or evolutionary strategies to minimise a loss measuring how well a candidate rule transforms a random network into one with the desired function — constrained by the function they implement rather than by experimental data. Jordan et al. do it with genetic programming, which has the advantage that it operates on analytically tractable, interpretable expressions and needs no gradients in the search space.

Read the 2025 follow-up for the failure mode, because it's the one you'll hit. Scaling to co-active rules of increasing complexity, the meta-learning strategy begins to fail: it is hard to devise loss functions that constrain network dynamics to plausible solutions a priori, and multiple degenerate solutions with identical network behaviour appear. As a local optimisation strategy, ES returns one solution at a time, which makes exploring that degeneracy cumbersome.

That's your problem in advance. Loss design is the hand-curation that doesn't go away, and degeneracy is why you'd want an archive rather than an optimiser.

- Confavreux et al., NeurIPS 2020 · Balancing complexity/performance/plausibility, PMC12021293 (2025) · Jordan et al., eLife 66273

## 4. Automating the verifier, not just the artifact

The half of the loop that would have caught your 2009 defect. `endive` infers inductive invariants for parameterized distributed protocols directly in TLA+, generating candidate lemma invariants and greedily selecting those eliminating the most counterexamples to induction — first such technique working on TLA+ natively rather than a lower-level IR. DistAI does data-driven invariant learning; IC3PO exploits symmetry and quantifier inference. There's also recent work on LLM-guided TLA+ proof automation (arXiv 2512.09758) and IC3-plus-LLM invariant synthesis.

Relevant caveat from that literature, and it's the same point I made about your README: TLC is primarily a debugging tool for small finite instances, not a tool for inferring inductive invariants.

## 5. Archive structure for the explore/exploit half

MAP-Elites is the right default because your problem has degeneracy (many mappings, many rule parameterisations) and you want the map, not the argmax. Quality-diversity methods are called illumination algorithms because they reveal the performance potential of different regions of the solution space rather than seeking a single global optimum.

For your case specifically, look at SAIL: surrogate-assisted illumination uses Gaussian-process surrogates and acquisition-map sampling to cut the number of expensive evaluations by orders of magnitude. Your evaluator is a distributed simulator running hundreds of reps per cell. That's exactly the expensive-evaluation regime SAIL targets.

POET is the one to read if you want the environment and the solution to co-evolve — generating the witness tasks alongside the mappings rather than fixing your task set by hand.

- MAP-Elites: Mouret & Clune, arXiv 1504.04909 · SAIL: arXiv 1806.05865 · POET: Wang et al., GECCO 2019

## 6. Understanding without a human understanding

ACDC systematises the interpretability workflow and automates the step of finding which circuit implements a behaviour, but the authors are candid about limits: it misses some classes of units, and its behaviour is very sensitive to hyperparameter and metric choice, giving non-robust performance in some settings. Edge attribution patching makes it cheap enough to scale — two forward passes and a backward pass instead of per-edge inference.

This is Root B3 in your priors doc: the bet that a mechanistic account is recoverable post hoc even when it wasn't available a priori.

---

**Where I'd actually start.** Cluster 2 plus cluster 5, on the concrete task you already have. Treat the (partition, placement, delay assignment) triple as the genome, your existing oracle as the fitness, and a MAP-Elites archive over (d_min, processor count, blocking fraction) as the map. That gives you the d_min sweep from the report *and* an automated search, from the same run, with no new evaluator to build.

Cluster 3 is the intellectually deeper one and it's where the selection-side gap lives, but Confavreux 2025 tells you the loss-design bottleneck is real and unsolved. Worth knowing before you commit to it rather than after.
