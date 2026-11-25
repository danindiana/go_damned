#!/usr/bin/env python3
"""Generate one TLA+ spec + config per scenario from DamnedWeighted.tla.tmpl.

Each scenario is a small spiking network. The generator computes its correct
spike train with the same sequential semantics as sequentialEDS in Damned.go
(time order; inhibitory before excitatory at equal time, then source neuron; fire when the
potential reaches threshold, reset to 0, at most one spike per neuron per
stamp; stamps above TMax dropped) and writes it into the spec as Truth.

Usage:  python3 gen.py                      # named scenarios -> DW_<name>.tla/.cfg
        python3 gen.py --random K SEED      # K random networks -> random/
"""
import heapq
from pathlib import Path

HERE = Path(__file__).parent

# Neurons are numbered from 1; source 0 is the environment (a stimulus).
# Each scenario: P, neurons {id: (theta, proc)}, synapses [(pre, post, w, d)],
# stimuli [(neuron, stamp, w)], tmax.
SCENARIOS = {
    # Damned.go buildCancellation, ids shifted by +1. Inhibitor remote from target.
    "cancel": dict(
        P=2,
        neurons={1: (4, 1), 2: (1, 1), 3: (1, 1), 4: (1, 2)},
        syn=[(2, 1, 2, 3), (3, 1, 2, 2), (4, 1, -3, 1)],
        stim=[(2, 0, 1), (3, 1, 1), (4, 2, 1)],
        tmax=4,
    ),
    # Damned.go buildFaninLocalInhib(2), ids +1. Inhibitor local to target.
    "fanin_local": dict(
        P=2,
        neurons={1: (2, 1), 2: (1, 2), 3: (1, 2), 4: (1, 1)},
        syn=[(2, 1, 2, 2), (3, 1, 2, 2), (4, 1, -4, 1)],
        stim=[(2, 0, 1), (3, 0, 1), (4, 1, 1)],
        tmax=3,
    ),
    # Smallest case found by fuzz_test.go: one local inhibitory synapse, the
    # excitation is a stimulus at the same stamp.
    "fuzz_min": dict(
        P=2,
        neurons={1: (2, 2), 2: (2, 2)},
        syn=[(1, 2, -4, 2)],
        stim=[(2, 2, 2), (1, 0, 2)],
        tmax=3,
    ),
    # Found by the long random search: two excitatory impacts reach neuron 1 at
    # t = 3 from different sources; their order decides whether it keeps a
    # leftover after firing. Checks the source-neuron tie-break end to end.
    "equal_stamp": dict(
        P=3,
        neurons={1: (2, 1), 2: (1, 3), 3: (3, 2), 4: (1, 3), 5: (2, 2)},
        syn=[(5, 1, 2, 3), (1, 3, 2, 2), (3, 1, 1, 3)],
        stim=[(1, 2, 2), (3, 0, 3), (5, 0, 2)],
        tmax=8,
    ),
    # Mixed: a local inhibitor and a remote inhibitor on two targets, plus a
    # local excitatory chain, over two processors.
    "mixed": dict(
        P=2,
        neurons={1: (2, 1), 2: (1, 1), 3: (1, 2), 4: (2, 2), 5: (1, 2)},
        syn=[(2, 1, -3, 1), (3, 1, 2, 1), (5, 4, -3, 1), (2, 4, 2, 1), (3, 5, 1, 1)],
        stim=[(2, 0, 1), (3, 0, 1), (5, 0, 1)],
        tmax=3,
    ),
}


def truth(sc):
    out = {n: [] for n in sc["neurons"]}
    for pre, post, w, d in sc["syn"]:
        out[pre].append((post, w, d))
    q, seq = [], 0
    for n, t, w in sc["stim"]:
        heapq.heappush(q, (t, 0 if w < 0 else 1, 0, seq, n, w)); seq += 1
    pot, fired_at, fired = {}, {}, []
    while q:
        t, _, src, _, n, w = heapq.heappop(q)
        if t > sc["tmax"]:
            continue
        pot[n] = pot.get(n, 0) + w
        if pot[n] >= sc["neurons"][n][0] and fired_at.get(n) != t + 1:
            fired.append((t, n)); fired_at[n] = t + 1; pot[n] = 0
            for post, w2, d in out[n]:
                heapq.heappush(q, (t + d, 0 if w2 < 0 else 1, n, seq, post, w2)); seq += 1
    return fired


def tla_set(items):
    return "{" + ", ".join(items) + "}"


def random_scenarios(k, seed):
    """k random small networks: 2-3 processors, 2-4 neurons, signed weights,
    delays 1-2, 1-3 stimuli. Same shape as fuzz_test.go's randCase, smaller."""
    import random
    r = random.Random(seed)
    out = {}
    while len(out) < k:
        P = 2 + (r.random() < 0.3)
        n = 2 + r.randrange(3)
        neurons = {i: (1 + r.randrange(2), 1 + r.randrange(P)) for i in range(1, n + 1)}
        syn, seen = [], set()
        for _ in range(n + r.randrange(n + 1)):
            a, b = r.randrange(1, n + 1), r.randrange(1, n + 1)
            if a == b or (a, b) in seen:
                continue
            seen.add((a, b))
            w = 1 + r.randrange(2) if r.random() < 0.6 else -(2 + r.randrange(3))
            syn.append((a, b, w, 1 + r.randrange(2)))
        if not syn:
            continue
        stim = [(r.randrange(1, n + 1), r.randrange(3), 0) for _ in range(1 + r.randrange(3))]
        # TLA+ holds events in sets, so identical stimuli would merge into one;
        # keep each (neuron, stamp) once so the model and Truth agree.
        stim = sorted({(nn, t): (nn, t, neurons[nn][0]) for nn, t, _ in stim}.values())
        sc = dict(P=P, neurons=neurons, syn=syn, stim=stim, tmax=4)
        if not truth(sc):
            continue  # nothing fires: uninformative
        out[f"r{len(out):03d}"] = sc
    return out


# Which guards get .cfg files, and whether only the one-at-a-time variant.
# Defaults: every guard, both variants. For a run of the paper readings alone:
#   GUARDS=2009P1,2009P2,2009P3 SERIAL_ONLY=1 python3 gen.py --random 120 2026 random_paper
import os
GUARDS = os.environ.get("GUARDS", "2009,2009L,2009P1,2009P2,2009P3").split(",")
SERIAL_ONLY = bool(os.environ.get("SERIAL_ONLY"))


def main():
    import sys
    tmpl = (HERE / "DamnedWeighted.tla.tmpl").read_text()
    scen = SCENARIOS
    outdir = HERE
    if len(sys.argv) > 1 and sys.argv[1] == "--random":
        k, seed = int(sys.argv[2]), int(sys.argv[3])
        scen = random_scenarios(k, seed)
        outdir = HERE / (sys.argv[4] if len(sys.argv) > 4 else "random")
        outdir.mkdir(exist_ok=True)
    write(tmpl, scen, outdir)


def write(tmpl, scen, outdir):
    HERE_ = outdir
    for name, sc in scen.items():
        N = sorted(sc["neurons"])
        dmin = min(d for *_, d in sc["syn"])
        defs = {
            "NAME": f"DW_{name}",
            "NPROC": str(sc["P"]),
            "NEURONS": "1.." + str(len(N)),
            "THETA": "[n \\in Neurons |-> " + _case({n: sc["neurons"][n][0] for n in N}) + "]",
            "OWNER": "[n \\in Neurons |-> " + _case({n: sc["neurons"][n][1] for n in N}) + "]",
            "SYN": tla_set([f"[pre |-> {a}, post |-> {b}, w |-> {w}, d |-> {d}]" for a, b, w, d in sc["syn"]]),
            "STIM": tla_set([f"[nj |-> {n}, ni |-> 0, st |-> {t}, w |-> {w}]" for n, t, w in sc["stim"]]),
            "TRUTH": tla_set([f"<<{t}, {n}>>" for t, n in truth(sc)]),
            "TMAX": str(sc["tmax"]),
            "DMIN": str(dmin),
        }
        spec = tmpl
        for k, v in defs.items():
            spec = spec.replace("@" + k + "@", v)
        (HERE_ / f"DW_{name}.tla").write_text(spec)
        for guard in GUARDS:
          for serial in ((True,) if SERIAL_ONLY else (False, True)):
            suffix = "_serial" if serial else ""
            (HERE_ / f"DW_{name}_{guard}{suffix}.cfg").write_text(
                f"CONSTANTS\n  GUARD = \"{guard}\"\n  MaxChan = 3\n  SERIAL = {'TRUE' if serial else 'FALSE'}\n"
                "SPECIFICATION Spec\n"
                "INVARIANTS TypeOK NoWrongSpike CompleteAtEnd\n"
                "CHECK_DEADLOCK TRUE\n")
        print(name, "truth:", truth(sc), "dmin:", dmin)


def _case(m):
    keys = sorted(m)
    if len(keys) == 1:
        return str(m[keys[0]])
    s = "CASE " + " [] ".join(f"n = {k} -> {m[k]}" for k in keys[:-1])
    return s + f" [] OTHER -> {m[keys[-1]]}"


if __name__ == "__main__":
    main()
