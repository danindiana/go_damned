#!/usr/bin/env python3
"""
damned_ref.py — an executable reconstruction of the DAMNED 2006 algorithm
(arXiv cs/0512018, sections 3-4), plus a ground-truth sequential event-driven
simulator to check it against.

Goal: transcribe the CMC and CPC authorization predicates *exactly as written*
in section 4, run a toy SNN, and see whether the distributed run reproduces the
sequential run's spike train. Where the paper's pseudocode is ambiguous, the
choice is flagged with  # AMBIGUITY:  so the reader can see what had to be
guessed.

This is a *simulation of the distributed system inside one process*: we model P
processors + Pr0 as objects, each with its own queues/clocks, and a scheduler
that interleaves their CMC/CPC loop steps. The scheduler order is controllable,
which lets us search for the interleaving that breaks the predicates.

Neuron model: the simplest thing that exhibits the delayed-firing problem —
a leaky-free integrate-and-fire with integer time. A neuron sums weighted
impacts; when the running sum crosses theta it forecasts a spike "now"; a
sufficiently negative (inhibitory) impact arriving earlier can pull the sum
back below theta and cancel a forecast that has not yet been committed.
"""

from __future__ import annotations
import heapq, itertools, argparse, random, sys
from dataclasses import dataclass, field
from typing import Optional

# --------------------------------------------------------------------------
# Network definition
# --------------------------------------------------------------------------

@dataclass
class Synapse:
    post: int
    w: int          # integer weight (signed; negative = inhibitory)
    d: int          # integer delay >= 1

@dataclass
class Neuron:
    nid: int
    theta: int
    out: list        # list[Synapse]
    proc: int        # processor that owns it

    @property
    def d_min(self):
        return min((s.d for s in self.out), default=1)

class Network:
    def __init__(self):
        self.neurons: dict[int, Neuron] = {}
    def add(self, nid, theta, proc):
        self.neurons[nid] = Neuron(nid, theta, [], proc)
    def connect(self, pre, post, w, d):
        assert d >= 1
        self.neurons[pre].out.append(Synapse(post, w, d))
    def d_min_global(self):
        return min(n.d_min for n in self.neurons.values())
    def d_max_global(self):
        return max((s.d for n in self.neurons.values() for s in n.out), default=1)

# --------------------------------------------------------------------------
# GROUND TRUTH: a plain sequential event-driven simulator.
# One global priority queue of impacts, ordered by (impact_time, is_excitatory)
# so that at equal time inhibition is applied before excitation — this is the
# rule DAMNED 2009 adds explicitly, and the physically correct one.
# A neuron accumulates impacts; each time its potential crosses theta it fires.
# Exact firing times here are the reference the distributed run must match.
# --------------------------------------------------------------------------

def sequential_eds(net: Network, stimuli: list[tuple[int,int,int]], t_max=30):
    """stimuli: list of (neuron, time, weight) external impacts.
    Returns sorted list of (time, neuron) firing events within [0, t_max]."""
    # event = (impact_time, exc_first_key, neuron, weight)
    # exc_first_key: 0 for inhibitory (w<0) so it sorts first, 1 for excitatory
    pq = []
    ctr = itertools.count()
    for (nid, t, w) in stimuli:
        heapq.heappush(pq, (t, 0 if w < 0 else 1, next(ctr), nid, w))
    pot = {nid: 0 for nid in net.neurons}
    fired = []
    fired_at = {}   # nid -> last fire time, to model a simple refractory of 1
    while pq:
        t, _key, _c, nid, w = heapq.heappop(pq)
        if t > t_max:
            break
        pot[nid] += w
        n = net.neurons[nid]
        if pot[nid] >= n.theta and fired_at.get(nid) != t:
            # fire
            fired.append((t, nid))
            fired_at[nid] = t
            pot[nid] = 0   # reset
            for s in n.out:
                heapq.heappush(pq, (t + s.d, 0 if s.w < 0 else 1,
                                    next(ctr), s.post, s.w))
    fired.sort()
    return fired

# --------------------------------------------------------------------------
# DISTRIBUTED RECONSTRUCTION (DAMNED 2006)
# --------------------------------------------------------------------------
#
# Event object encodings from section 2:
#   CM event  = [target Nj, source Ni, stamp st_i]      (incoming spike to compute)
#   CP event  = [source Ni, stamp st_i, crt]            (outgoing spike to emit)
#
# Per-processor state from sections 3-4:
#   CM queue  : priority queue of CM events by st_i
#   CP queue  : priority queue of CP events by st_i
#   et_p      : emission time  (signed; <0 == CP queue empty)
#   pt_p      : processing time (signed; <0 == CM queue empty)
#   nbth_p    : number of active EC threads
#   Clk(p)    : array et_0..et_P of last-known emission times
#
# We model EC "threads" as instantaneous computations that produce a CP event
# or nothing. Concurrency (nbth_p) is modelled by letting the scheduler start a
# computation (nbth++) and finish it (nbth--) as separate steps, so an
# interleaving can leave a neuron "mid-computation" while other steps run.

INF = 10**9

@dataclass(order=True)
class CMEvent:
    st: int
    nj: int = field(compare=False)
    ni: int = field(compare=False)

@dataclass(order=True)
class CPEvent:
    st: int
    ni: int = field(compare=False)
    crt: bool = field(compare=False, default=False)

class Processor:
    def __init__(self, pid, net, P):
        self.pid = pid
        self.net = net
        self.P = P
        self.cmq: list[CMEvent] = []
        self.cpq: list[CPEvent] = []
        self.et = 0
        self.pt = 0
        self.nbth = 0
        self.clk = [0]*(P+1)              # index 0..P
        self.inbox: list[tuple[int,list]] = []   # (sender_clk_snapshotidx, [CMEvent], clkcopy)
        self.pending_threads = []        # list of (nj, ni, st) currently "running"
        self.local_neurons = {nid for nid,n in net.neurons.items() if n.proc==pid}
        self.pot = {nid:0 for nid in self.local_neurons}
        self.fired_at = {}
        # bookkeeping of emitted CP events still in queue, for certification
        # committed emissions recorded by the harness

# --------------------------------------------------------------------------
# The distributed engine. All processor states live here; the scheduler picks
# which processor does which loop-step next. Messages are delivered via inboxes.
# --------------------------------------------------------------------------

class Damned2006:
    def __init__(self, net: Network, stimuli, seed=0, strategy="fair", t_max=30):
        self.net = net
        self.t_max = t_max
        nprocs = 1 + max(n.proc for n in net.neurons.values())
        self.P = nprocs - 1   # processors are 1..P ; Pr0 is environment
        self.procs = {p: Processor(p, net, self.P) for p in range(1, self.P+1)}
        self.T = 0
        self.et0 = 0
        self.clk0 = [0]*(self.P+1)
        self.dmin = net.d_min_global()
        self.stimuli = sorted(stimuli, key=lambda x: x[1])
        self.committed = []       # (time, nid) actually emitted — the distributed spike train
        self.msgbus = []          # in-flight messages: (dst, [CMEvent], clkcopy)
        self.rng = random.Random(seed)
        self.strategy = strategy
        self.log = []
        self.deadlock_timeouts = 0
        self.violations = []

    # ---- environment processor Pr0 -------------------------------------
    def pr0_inject(self):
        """Pr0 increments T and injects stimuli with stamp T-1 (section 4)."""
        self.T += 1
        self.et0 = self.T
        self.clk0[0] = self.et0
        # deliver external CM events with stamp T-1 to owning processors
        for (nid, t, w) in self.stimuli:
            if t == self.T - 1:
                p = self.net.neurons[nid].proc
                # external impact modelled as a CM event whose "source" is Ext(-1)
                self.msgbus.append((p, [("EXT", nid, t, w)], list(self.clk0)))
        # broadcast clock to everyone (section 4: everyone learns et_0)
        for p in range(1, self.P+1):
            self.msgbus.append((p, [], list(self.clk0)))

    # ---- message delivery ----------------------------------------------
    def deliver(self):
        moved = False
        for (dst, payload, clk) in self.msgbus:
            self.procs[dst].inbox.append((payload, clk))
            moved = True
        self.msgbus.clear()
        return moved

    # ---- CMC step 1: message reception ---------------------------------
    def cmc_reception(self, p):
        pr = self.procs[p]
        if not pr.inbox:
            return False
        acted = False
        for (payload, clk) in pr.inbox:
            # update clock knowledge: (∀m≠p) if |et_m(q)|>=|et_m(p)| then adopt
            for m in range(0, self.P+1):
                if m == p: continue
                if abs(clk[m]) >= abs(pr.clk[m]):
                    pr.clk[m] = clk[m]
            for ev in payload:
                if isinstance(ev, tuple) and ev[0] == "EXT":
                    _tag, nid, t, w = ev
                    heapq.heappush(pr.cmq, CMEvent(t, nid, -1))
                    pr._ext_w = getattr(pr, "_ext_w", {})
                    pr._ext_w[(nid, t, -1)] = w
                else:
                    heapq.heappush(pr.cmq, ev)
                acted = True
            if payload:
                pr.pt = abs(pr.pt)   # section 4: reception sets pt_p <- |pt_p|
        pr.inbox.clear()
        return acted

    # ---- CPC step 1: computation starter (authorization from section 4) --
    def cpc_computation_starter(self, p):
        pr = self.procs[p]
        if not pr.cmq:
            return False
        top = pr.cmq[0]
        st_i, nj, ni = top.st, top.nj, top.ni
        # AMBIGUITY: "thread associated to EC_j already active" — we check whether
        # nj is currently mid-computation in pending_threads.
        if any(th[0] == nj for th in pr.pending_threads):
            return False  # delayed; priority status handled by result collector
        authorized = False
        if st_i == pr.pt:
            authorized = True
        elif pr.nbth == 0:
            # (∀m)[st_i <= et_m or et_m <= 0]
            if all(st_i <= pr.clk[m] or pr.clk[m] <= 0 for m in range(0, self.P+1)):
                authorized = True
            else:
                # local deadlock detection:
                # if et_p < st_i and (∀m≠p)[st_i <= et_m or et_m <= 0]
                if pr.et < st_i and all(
                        st_i <= pr.clk[m] or pr.clk[m] <= 0
                        for m in range(0, self.P+1) if m != p):
                    st_l = pr.cpq[0].st if pr.cpq else INF
                    if st_i <= st_l:
                        authorized = True
        if not authorized:
            return False
        heapq.heappop(pr.cmq)
        pr.pt = st_i                 # ptp <- sti
        pr.nbth += 1                 # nbth++
        if ni == -1:
            w = pr._ext_w.get((nj, st_i, -1), 1) if hasattr(pr, "_ext_w") else 1
        else:
            # impact weight is the weight of the synapse ni -> nj
            w = next((s.w for s in self.net.neurons[ni].out if s.post == nj), 0)
        pr.pending_threads.append((nj, st_i, ni, w))
        if not pr.cmq:
            pr.pt = -abs(pr.pt)      # CM queue empty -> ptp <- -ptp
        return True

    # ---- EC thread body + CPC step 2: result collector ------------------
    def cpc_result_collector(self, p):
        pr = self.procs[p]
        if not pr.pending_threads:
            return False
        nj, st_i, ni, w = pr.pending_threads.pop(0)
        pr.nbth -= 1
        if nj not in pr.local_neurons:
            return True
        pr.pot[nj] += w
        n = self.net.neurons[nj]
        produced = None
        if pr.pot[nj] >= n.theta and pr.fired_at.get(nj) != st_i:
            pr.fired_at[nj] = st_i
            pr.pot[nj] = 0
            produced = CPEvent(st=st_i, ni=nj, crt=False)
            heapq.heappush(pr.cpq, produced)
            if len(pr.cpq) == 1 and pr.et < 0:
                pr.et = abs(pr.et)   # CP queue was empty -> et_p <- |et_p|
        # certification of old CP events (section 4):
        # crt <- true when st_j <= currently processed st_i + d_min_j
        for ev in pr.cpq:
            dmin_j = self.net.neurons[ev.ni].d_min
            if ev.st <= st_i + dmin_j:
                ev.crt = True
        return True

    # ---- CMC step 2: emission control (authorization from section 4) -----
    def cmc_emission_control(self, p):
        pr = self.procs[p]
        if not pr.cpq:
            return False
        top = pr.cpq[0]
        st_i, ni, crt = top.st, top.ni, top.crt
        authorized = False
        if st_i == pr.et:
            authorized = True
        elif crt:
            authorized = True
        elif st_i <= pr.pt:
            authorized = True
        elif pr.nbth == 0:
            if pr.pt < 0 and all(
                    st_i <= pr.clk[m] or pr.clk[m] <= 0
                    for m in range(0, self.P+1) if m != p):
                authorized = True
        if not authorized:
            return False
        heapq.heappop(pr.cpq)
        pr.et = st_i                          # etp <- sti
        # ---- CHECK EXACTNESS AT THE COMMIT POINT ----
        # A committed emission is a claim: neuron ni truly fires at st_i.
        self.committed.append((st_i, ni))
        # generate CM events for all postsynaptic targets
        made_local = False
        for s in self.net.neurons[ni].out:
            if st_i + s.d > self.t_max:   # respect the simulation horizon
                continue
            ev = CMEvent(st=st_i + s.d, nj=s.post, ni=ni)
            tgt = self.net.neurons[s.post].proc
            if tgt == p:
                heapq.heappush(pr.cmq, ev); made_local = True
            else:
                self.msgbus.append((tgt, [ev], list(pr.clk)))
            # also send to Pr0 if target is an output — omitted; Pr0 modelled minimally
        if made_local:
            pr.pt = abs(pr.pt)
        if not pr.cpq:
            pr.et = -abs(pr.et)               # CP queue empty -> etp <- -etp
        # propagate our own et into our clock array
        pr.clk[p] = pr.et
        return True

    # ---- one scheduler step --------------------------------------------
    def step(self):
        """Return True if any processor made progress."""
        self.deliver()
        steps = []
        for p in range(1, self.P+1):
            steps.append(("cmc_recv", p, self.cmc_reception))
            steps.append(("cpc_start", p, self.cpc_computation_starter))
            steps.append(("cpc_collect", p, self.cpc_result_collector))
            steps.append(("cmc_emit", p, self.cmc_emission_control))
        if self.strategy == "adversarial":
            self.rng.shuffle(steps)
        progress = False
        for (_name, p, fn) in steps:
            if fn(p):
                progress = True
        return progress

    def run(self, max_T=200, max_idle=50):
        self.pr0_inject()  # T -> 1
        idle = 0
        while self.T < max_T:
            moved = self.step()
            if moved:
                idle = 0
            else:
                idle += 1
                # nothing progressed: Pr0 advances (its time-out / next increment)
                self.pr0_inject()
                self.deadlock_timeouts += 1
                if self.T >= max_T or self.deadlock_timeouts > max_T:
                    break
            # stop when everything is drained and no external stimuli remain
            drained = all(not pr.cmq and not pr.cpq and not pr.pending_threads
                          and not pr.inbox for pr in self.procs.values())
            no_more_stim = all(t < self.T for (_n,t,_w) in self.stimuli)
            if drained and no_more_stim and not self.msgbus:
                break
        self.committed.sort()
        # dedup (a spike may be legitimately committed once)
        seen = set(); out=[]
        for e in self.committed:
            if e not in seen:
                seen.add(e); out.append(e)
        return out

# --------------------------------------------------------------------------
# Toy networks
# --------------------------------------------------------------------------

def net_figure1():
    """The paper's Figure 1: one postsynaptic neuron Nj fed by four presynaptic
    neurons, three excitatory and one inhibitory-with-small-delay that cancels
    a forecast. We place the inhibitory source on a *different* processor to
    exercise the cross-processor straggler case."""
    net = Network()
    # Nj = 0 owned by proc 1 ; presynaptics 1..4
    net.add(0, theta=3, proc=1)      # postsynaptic, needs 3 to fire
    net.add(1, theta=1, proc=1)      # exc source, delay 1
    net.add(2, theta=1, proc=1)      # exc source, delay 2
    net.add(4, theta=1, proc=1)      # exc source, delay 4
    net.add(3, theta=1, proc=2)      # INHIBITORY source on a DIFFERENT processor
    net.connect(1, 0, w=+2, d=1)
    net.connect(2, 0, w=+2, d=2)
    net.connect(4, 0, w=+2, d=4)
    net.connect(3, 0, w=-5, d=1)     # strong, short-delay inhibition
    return net

def stim_figure1():
    # fire the sources at the times from Figure 1
    return [(1,0,+1),(2,1,+1),(4,3,+1),(3,4,+1)]

def net_cancellation():
    """The clean cancellation counterexample. Postsynaptic N0 (theta=4) receives
    two excitatory impacts that together reach threshold at t=3, and one
    inhibitory impact (from a source on a DIFFERENT processor) that also lands at
    t=3 with a *smaller* delay. In a correct (inhibition-first) run N0 never
    fires. If processor 1 computes N0 on a stale clock -- not yet knowing the
    remote inhibition -- it will commit a spike that does not exist.
    Sources fire once (theta=1, single +1 kick)."""
    net = Network()
    net.add(0, theta=4, proc=1)     # postsynaptic
    net.add(1, theta=1, proc=1)     # exc, fires t=0, d=3 -> impact t=3
    net.add(2, theta=1, proc=1)     # exc, fires t=1, d=2 -> impact t=3
    net.add(3, theta=1, proc=2)     # INHIB on proc 2, fires t=2, d=1 -> impact t=3
    net.connect(1, 0, w=+2, d=3)
    net.connect(2, 0, w=+2, d=2)
    net.connect(3, 0, w=-3, d=1)
    return net

def stim_cancellation():
    return [(1,0,+1),(2,1,+1),(3,2,+1)]

def net_random(nn=15, pe=0.18, P=3, seed=1):
    rng = random.Random(seed)
    net = Network()
    for i in range(nn):
        net.add(i, theta=rng.choice([1,2,2]), proc=1+(i % P))
    for i in range(nn):
        for j in range(nn):
            if i!=j and rng.random() < pe:
                w = rng.choice([+2,+2,+2,+2,-3])   # mostly excitatory + some inhibition
                net.connect(i, j, w=w, d=rng.randint(1,4))
    return net

def stim_random(nn=30, k=10, seed=2):
    rng = random.Random(seed)
    # strong external kicks (weight 2) so theta-1/2 neurons actually fire
    return [(rng.randrange(nn), rng.randint(0,3), +2) for _ in range(k)]

# --------------------------------------------------------------------------
# Harness
# --------------------------------------------------------------------------

def compare(name, net, stimuli, trials=200, t_max=30):
    truth = sequential_eds(net, stimuli, t_max=t_max)
    print(f"\n=== {name} ===")
    print(f"  ground-truth sequential EDS: {len(truth)} spikes -> {truth[:12]}{'...' if len(truth)>12 else ''}")
    # fair scheduler
    d = Damned2006(net, stimuli, strategy="fair", t_max=t_max)
    got = d.run(max_T=t_max+5)
    ok = (got == truth)
    print(f"  DAMNED-2006 (fair sched)  : {len(got)} spikes, match={ok}, timeouts={d.deadlock_timeouts}")
    if not ok:
        print(f"      distributed: {got[:12]}")
    # adversarial search
    worst = None
    mism = 0
    for s in range(trials):
        d = Damned2006(net, stimuli, seed=s, strategy="adversarial", t_max=t_max)
        got = d.run(max_T=t_max+5)
        if got != truth:
            mism += 1
            if worst is None:
                worst = (s, got)
    print(f"  adversarial interleavings : {mism}/{trials} diverged from ground truth")
    if worst:
        s, got = worst
        extra = [e for e in got if e not in truth]
        missing = [e for e in truth if e not in got]
        print(f"      first divergent seed={s}")
        print(f"      spurious spikes (committed but not real): {extra[:8]}")
        print(f"      missing spikes  (real but not committed): {missing[:8]}")
    return truth, ok, mism

if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--trials", type=int, default=300)
    args = ap.parse_args()
    compare("Cancellation counterexample (remote inhibition, should suppress the spike)",
            net_cancellation(), stim_cancellation(), trials=args.trials)
    compare("Figure 1 (mis-timed spike under stale clocks)",
            net_figure1(), stim_figure1(), trials=args.trials)
    compare("Random SNN, 15 neurons, 3 processors",
            net_random(nn=15), stim_random(nn=15, k=5), trials=min(args.trials,120))
