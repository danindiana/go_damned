------------------------------- MODULE DW_fuzz_min -------------------------------
(***************************************************************************)
(* DamnedWeighted: a model of the go_damned engine with real neurons.        *)
(* Generated from DamnedWeighted.tla.tmpl by gen.py; edit the template.      *)
(*                                                                          *)
(* Differences from DamnedFull2.tla, and why:                               *)
(*  - Neurons have integer potentials, thresholds and signed synaptic       *)
(*    weights, so integrating events in the wrong order changes the result. *)
(*  - Events are taken in time order, inhibitory before excitatory at equal *)
(*    time, exactly as Damned.go's cmQueue.pop does.                        *)
(*  - The invariant compares the committed spikes with the correct spike    *)
(*    train (Truth, computed by gen.py with the same rules as sequentialEDS)*)
(*    instead of checking for pending events at commit time.                *)
(*                                                                          *)
(* Simplifications (stated so the result is read at the right strength):    *)
(*  - Stimuli are placed in their owners' queues at the start; the          *)
(*    environment process Pr0 sends nothing else and never bounds anyone.   *)
(*  - One fixed network per spec (see gen.py), not all networks.            *)
(*  - Channels hold at most MaxChan messages.                               *)
(*                                                                          *)
(* SERIAL = TRUE   : at most one computation per processor at a time, as   *)
(*                   in the 2009 paper ("CPC processes the top event").     *)
(* SERIAL = FALSE  : one computation per neuron at a time, as Damned.go.    *)
(*                                                                          *)
(* GUARD = "2009"  : computation starts only when the event's stamp is      *)
(*                   strictly below every peer's advertised bound.          *)
(* GUARD = "2009L" : the same, AND strictly below (earliest local forecast  *)
(*                   or running computation) + DMIN.                        *)
(***************************************************************************)
EXTENDS Integers, FiniteSets, Sequences, TLC

CONSTANTS GUARD, MaxChan, SERIAL

NPROC   == 2
Procs   == 1..NPROC
Neurons == 1..2
THETA   == [n \in Neurons |-> CASE n = 1 -> 2 [] OTHER -> 2]
OWNER   == [n \in Neurons |-> CASE n = 1 -> 2 [] OTHER -> 2]
SYN     == {[pre |-> 1, post |-> 2, w |-> -4, d |-> 2]}
STIM    == {[nj |-> 2, ni |-> 0, st |-> 2, w |-> 2], [nj |-> 1, ni |-> 0, st |-> 0, w |-> 2]}
TRUTH   == {<<0, 1>>}
TMAX    == 3
DMIN    == 2
INF     == TMAX + 1   \* any bound past TMAX means "nothing more that matters"

Peers(p) == Procs \ {p}
Min(S)   == CHOOSE x \in S : \A y \in S : x =< y
Exc(e)   == IF e.w < 0 THEN 0 ELSE 1

VARIABLES cm, running, cp, pot, firedAt, kb, chan, sentB, committed
vars == <<cm, running, cp, pot, firedAt, kb, chan, sentB, committed>>

(* Earliest impact stamp p's future emissions could produce: its own pending *)
(* stamps and everything peers may still send it, plus the lookahead DMIN.   *)
BoundOf(p, cmS, runS, cpS) ==
    LET all == { e.st : e \in cmS \cup runS } \cup { c.st : c \in cpS }
               \cup { kb[p][m] : m \in Peers(p) }
        mm  == Min(all \cup {INF})
    IN IF mm >= INF THEN INF ELSE mm + DMIN

MyBound(p) == BoundOf(p, cm[p], running[p], cp[p])

(* Events with the least (stamp, inhibitory-first) key. *)
MinKey(S) == { e \in S : \A f \in S : e.st < f.st \/ (e.st = f.st /\ Exc(e) =< Exc(f)) }

CompAuth(p, st) ==
    /\ \A m \in Peers(p) : st < kb[p][m]
    /\ IF GUARD = "2009L"
         THEN LET own == { c.st : c \in cp[p] } \cup { r.st : r \in running[p] }
              IN IF own = {} THEN TRUE ELSE st < Min(own) + DMIN
         ELSE TRUE

LocalClear(p, n, s) == ~ \E e \in cm[p] \cup running[p] : e.nj = n /\ e.st =< s

EmitAuth(p, c) ==
    /\ LocalClear(p, c.ni, c.st)
    /\ \A m \in Peers(p) : c.st < kb[p][m]

Init ==
    /\ cm        = [p \in Procs |-> { e \in STIM : OWNER[e.nj] = p }]
    /\ running   = [p \in Procs |-> {}]
    /\ cp        = [p \in Procs |-> {}]
    /\ pot       = [n \in Neurons |-> 0]
    /\ firedAt   = [n \in Neurons |-> -1]
    /\ kb        = [p \in Procs |-> [m \in Procs |-> 0]]
    /\ chan      = [s \in Procs |-> [d \in Procs |-> << >>]]
    /\ sentB     = [p \in Procs |-> [q \in Procs |-> -1]]
    /\ committed = {}

(* CPC 1: start computing the least event, if the guard allows it. *)
Start(p) ==
    /\ cm[p] # {}
    /\ \E e \in MinKey(cm[p]) :
         /\ CompAuth(p, e.st)
         /\ ~ \E r \in running[p] : r.nj = e.nj
         /\ IF SERIAL THEN running[p] = {} ELSE TRUE
         /\ cm'      = [cm EXCEPT ![p] = @ \ {e}]
         /\ running' = [running EXCEPT ![p] = @ \cup {e}]
    /\ UNCHANGED <<cp, pot, firedAt, kb, chan, sentB, committed>>

(* EC body + CPC 2: integrate the impact; forecast a spike on threshold. *)
Finish(p) ==
    /\ \E e \in running[p] :
         LET v == pot[e.nj] + e.w IN
         /\ running' = [running EXCEPT ![p] = @ \ {e}]
         /\ IF v >= THETA[e.nj] /\ firedAt[e.nj] # e.st + 1
              THEN /\ pot'     = [pot EXCEPT ![e.nj] = 0]
                   /\ firedAt' = [firedAt EXCEPT ![e.nj] = e.st + 1]
                   /\ cp'      = [cp EXCEPT ![p] = @ \cup {[ni |-> e.nj, st |-> e.st]}]
              ELSE /\ pot'     = [pot EXCEPT ![e.nj] = v]
                   /\ UNCHANGED <<firedAt, cp>>
    /\ UNCHANGED <<cm, kb, chan, sentB, committed>>

(* CMC: commit the least forecast spike, if authorized; fan out impacts. *)
Emit(p) ==
    /\ cp[p] # {}
    /\ \E c \in { x \in cp[p] : \A y \in cp[p] : x.st =< y.st } :
         LET outs   == { [nj |-> s.post, ni |-> c.ni, st |-> c.st + s.d, w |-> s.w] :
                         s \in { s \in SYN : s.pre = c.ni } }
             valid  == { o \in outs : o.st =< TMAX }
             local  == { o \in valid : OWNER[o.nj] = p }
             tgts   == { q \in Peers(p) : \E o \in valid : OWNER[o.nj] = q }
             newCp  == cp[p] \ {c}
             newCm  == cm[p] \cup local
             b      == BoundOf(p, newCm, running[p], newCp)
         IN /\ EmitAuth(p, c)
            /\ \A q \in tgts : Len(chan[p][q]) < MaxChan
            /\ cp'        = [cp EXCEPT ![p] = newCp]
            /\ cm'        = [cm EXCEPT ![p] = newCm]
            /\ committed' = committed \cup { <<c.st, c.ni>> }
            /\ chan' = [chan EXCEPT ![p] =
                         [q \in Procs |-> IF q \in tgts
                            THEN Append(chan[p][q],
                                   [evs |-> { o \in valid : OWNER[o.nj] = q }, bnd |-> b])
                            ELSE chan[p][q]]]
            /\ sentB' = [sentB EXCEPT ![p] = [q \in Procs |-> IF q \in tgts THEN b ELSE @[q]]]
    /\ UNCHANGED <<running, pot, firedAt, kb>>

(* Null message: advertise a changed bound to one peer. *)
NullSend(p, q) ==
    /\ q # p
    /\ MyBound(p) # sentB[p][q]
    /\ Len(chan[p][q]) < MaxChan
    /\ chan'  = [chan EXCEPT ![p][q] = Append(@, [evs |-> {}, bnd |-> MyBound(p)])]
    /\ sentB' = [sentB EXCEPT ![p][q] = MyBound(p)]
    /\ UNCHANGED <<cm, running, cp, pot, firedAt, kb, committed>>

(* CMC: receive the head message from q (FIFO per ordered pair). *)
Receive(p, q) ==
    /\ q # p
    /\ chan[q][p] # << >>
    /\ LET m == Head(chan[q][p]) IN
         /\ cm'   = [cm EXCEPT ![p] = @ \cup m.evs]
         /\ kb'   = [kb EXCEPT ![p][q] = m.bnd]
         /\ chan' = [chan EXCEPT ![q][p] = Tail(@)]
    /\ UNCHANGED <<running, cp, pot, firedAt, sentB, committed>>

Terminated ==
    /\ \A p \in Procs : cm[p] = {} /\ running[p] = {} /\ cp[p] = {}
    /\ \A s, d \in Procs : chan[s][d] = << >>

Done == Terminated /\ UNCHANGED vars

Next ==
    \/ \E p \in Procs : Start(p) \/ Finish(p) \/ Emit(p)
    \/ \E p, q \in Procs : NullSend(p, q) \/ Receive(p, q)
    \/ Done

Spec == Init /\ [][Next]_vars

TypeOK == /\ committed \subseteq (0..TMAX) \X Neurons
          /\ \A p \in Procs : cp[p] \subseteq [ni : Neurons, st : 0..TMAX]

(* Safety: never commit a spike the sequential simulation does not produce. *)
NoWrongSpike == committed \subseteq TRUTH

(* Completeness: when everything has drained, every correct spike was committed. *)
CompleteAtEnd == Terminated => committed = TRUTH
=============================================================================
