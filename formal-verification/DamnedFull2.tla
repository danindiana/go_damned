------------------------------- MODULE DamnedFull2 -------------------------------
(***************************************************************************)
(* Full P-processor model of DAMNED, v2.                                    *)
(*                                                                          *)
(* Two corrections over v1, both forced by TLC:                             *)
(*                                                                          *)
(*  (1) CHANNELS ARE FIFO. In v1 chan[p] was a set, so a clock-update        *)
(*      message could overtake the event message it was meant to guard.     *)
(*      Real DAMNED rides on MPI, which guarantees ordering between a given  *)
(*      sender/receiver pair. Modelling channels as sequences makes that     *)
(*      assumption explicit -- and it turns out the 2009 discipline NEEDS    *)
(*      it. This is exactly the per-channel FIFO requirement of CMB.         *)
(*                                                                          *)
(*  (2) THE 2009 BOUND IS AN LBTS, NOT A FLAG. A processor's advertised      *)
(*      bound must be a genuine lower bound on every emission it could       *)
(*      still make: the minimum stamp over its CP queue (spikes it will      *)
(*      emit) and its CM/delayed/running events (spikes they may produce).   *)
(*      If it has nothing at all, the bound is +infinity ("do not wait for   *)
(*      me"). In v1 I collapsed this to the sentinel 0, which collided with  *)
(*      the initial clock value and made the guard vacuous.                  *)
(*                                                                          *)
(* FIX = FALSE : 2006 guard, "st_i <= et_m  OR  et_m <= 0".                  *)
(* FIX = TRUE  : 2009 discipline, "st_i <= LBTS(m)" for every peer m.        *)
(***************************************************************************)
EXTENDS Integers, FiniteSets, Sequences, TLC

CONSTANTS P, MaxT, DMax, FIX, MaxChan

Procs  == 1..P
All    == 0..P
Nrn    == 1..(2*P)
Stamps == 0..MaxT
INF    == MaxT + 1          \* "nothing pending; do not wait for me"

Abs(x) == IF x < 0 THEN -x ELSE x
Owner(n) == ((n-1) % P) + 1
Wrap(n)  == ((n % (2*P)) + 1)
Post(n)  == { [tgt |-> Wrap(n),       d |-> 1],
              [tgt |-> Wrap(Wrap(n)), d |-> 2] }

CMEvent == [nj : Nrn, ni : Nrn \cup {0}, st : Stamps]   \* st = IMPACT time
CPEvent == [ni : Nrn, st : Stamps, crt : BOOLEAN]

VARIABLES cmq, dly, cpq, et, pt, nbth, clk, kb, chan, T, et0, running, bad
vars == <<cmq,dly,cpq,et,pt,nbth,clk,kb,chan,T,et0,running,bad>>

MinOf(S) == CHOOSE x \in S : \A y \in S : x =< y
MinSt(S) == CHOOSE e \in S : \A f \in S : e.st =< f.st

(*-------------------------------------------------------------------------*)
(* LBTS: a lower bound on every emission processor p could still make.      *)
(* Anything in cpq will be emitted at its stamp; anything pending in        *)
(* cmq/dly/running may produce an emission at its impact stamp.             *)
(*-------------------------------------------------------------------------*)
MyBound(p) ==
    (* A processor may NOT advertise "nothing pending" just because its own
       queues are empty: new work can still arrive from Pr0 or from a peer,
       and it would then emit in that peer's past. The bound must therefore be
       derived from what p can still be TOLD about -- the minimum of its own
       pending stamps and every peer's advertised bound -- plus the lookahead
       dmin (= 1 here, since every synaptic delay is >= 1). This is precisely
       CMB: a process's output bound is min(input channel clocks) + lookahead. *)
    LET own  == { e.st : e \in (cmq[p] \cup dly[p] \cup running[p]) }
                \cup { c.st : c \in cpq[p] }
        ins  == { kb[p][m] : m \in All \ {p} }
        base == MinOf(own \cup ins)
    IN IF base + 1 >= INF THEN INF ELSE base + 1

(* Pr0 will inject at stamps >= T while it can still inject. *)
Bound0 == IF T < MaxT THEN T ELSE INF

(*-------------------------------------------------------------------------*)
(* The causality predicate: is a forecast by neuron n at stamp s still      *)
(* cancellable by something unaccounted for anywhere in the system?         *)
(*-------------------------------------------------------------------------*)
InFlight(n, s) ==
    \E q \in Procs : \E i \in 1..Len(chan[q]) :
        \E e \in chan[q][i].evs : e.nj = n /\ e.st =< s

PendingFor(n, s) ==
    \/ \E q \in Procs : \E e \in cmq[q]     : e.nj = n /\ e.st =< s
    \/ \E q \in Procs : \E e \in dly[q]     : e.nj = n /\ e.st =< s
    \/ \E q \in Procs : \E e \in running[q] : e.nj = n /\ e.st =< s
    \/ InFlight(n, s)

(* The locally-checkable half of No-Straggler: no unprocessed event on THIS
   processor can still land on neuron n at or before stamp s. In the real
   algorithm this is what the pt_p conditions approximate. *)
LocalClear(p, n, s) ==
    ~ \E x \in (cmq[p] \cup dly[p] \cup running[p]) : x.nj = n /\ x.st =< s

Msg(src, evs, c, b) == [src |-> src, evs |-> evs, clk |-> c, bnd |-> b]

CanSend(q) == Len(chan[q]) < MaxChan

(*=========================================================================*)
(* Pr0                                                                      *)
(*=========================================================================*)
Pr0Inject ==
    /\ T < MaxT
    /\ \A q \in Procs : CanSend(q)
    /\ T' = T + 1
    /\ et0' = T + 1
    /\ \E n \in Nrn :
         LET ev == [nj |-> n, ni |-> 0, st |-> T]
             c  == [m \in All |-> IF m = 0 THEN T+1 ELSE 0]
             b  == IF T + 1 < MaxT THEN T + 1 ELSE INF
         IN chan' = [q \in Procs |->
                       IF q = Owner(n)
                         THEN Append(chan[q], Msg(0, {ev}, c, b))
                         ELSE Append(chan[q], Msg(0, {}, c, b))]
    /\ UNCHANGED <<cmq,dly,cpq,et,pt,nbth,clk,kb,running,bad>>

(* The 2006 time-out: advance T and FABRICATE et_p(0) <- T-1 for everyone. *)
Pr0Timeout ==
    /\ ~FIX
    /\ T < MaxT
    /\ \A q \in Procs : CanSend(q)
    /\ T' = T + 1
    /\ et0' = T + 1
    /\ LET c == [m \in All |-> IF m = 0 THEN T+1 ELSE T]
           b == IF T + 1 < MaxT THEN T + 1 ELSE INF
       IN chan' = [q \in Procs |-> Append(chan[q], Msg(0, {}, c, b))]
    /\ UNCHANGED <<cmq,dly,cpq,et,pt,nbth,clk,kb,running,bad>>

(* A pure null message: p advertises its current bound to everyone.
   Present in the 2009 algorithm (clock sent "eventually without events"). *)
NullSend(p) ==
    /\ FIX
    /\ \A q \in Procs \ {p} : CanSend(q)
    /\ LET b == MyBound(p)
           c == [m \in All |-> IF m = p THEN et[p] ELSE clk[p][m]]
       IN chan' = [q \in Procs |->
                     IF q = p THEN chan[q] ELSE Append(chan[q], Msg(p, {}, c, b))]
    /\ UNCHANGED <<cmq,dly,cpq,et,pt,nbth,clk,kb,T,et0,running,bad>>

(*=========================================================================*)
(* CMC 1 -- reception: FIFO head, clock merge, bound merge, delayed queue   *)
(*=========================================================================*)
Receive(p) ==
    /\ Len(chan[p]) > 0
    /\ LET m == Head(chan[p]) IN
        /\ chan' = [chan EXCEPT ![p] = Tail(@)]
        /\ clk'  = [clk EXCEPT ![p] =
             [k \in All |-> IF k # p /\ Abs(m.clk[k]) >= Abs(clk[p][k])
                              THEN m.clk[k] ELSE clk[p][k]]]
        (* bounds only ever increase; adopt the fresher (larger) one.
           We conservatively attribute the message's bound to Pr0 as well,
           since Pr0's broadcasts carry Bound0. *)
        (* FIFO per channel => the latest message from src is the freshest.
           Bounds are NOT monotonic (a peer's LBTS drops when it acquires new
           low-stamped work), so we OVERWRITE rather than take a maximum. *)
        /\ kb' = [kb EXCEPT ![p] =
             [k \in All |-> IF k = m.src THEN m.bnd ELSE kb[p][k]]]
        /\ LET ready == { e \in m.evs : e.st =< Abs(pt[p]) + DMax }
               late  == m.evs \ ready
           IN /\ cmq' = [cmq EXCEPT ![p] = @ \cup ready]
              /\ dly' = [dly EXCEPT ![p] = @ \cup late]
        /\ pt' = [pt EXCEPT ![p] = IF m.evs = {} THEN @ ELSE Abs(@)]
    /\ UNCHANGED <<cpq,et,nbth,T,et0,running,bad>>

Promote(p) ==
    /\ \E e \in dly[p] :
         /\ e.st =< Abs(pt[p]) + DMax
         /\ dly' = [dly EXCEPT ![p] = @ \ {e}]
         /\ cmq' = [cmq EXCEPT ![p] = @ \cup {e}]
         /\ pt'  = [pt EXCEPT ![p] = Abs(@)]
    /\ UNCHANGED <<cpq,et,nbth,clk,kb,chan,T,et0,running,bad>>

(*=========================================================================*)
(* CPC 1 -- computation starter                                             *)
(*=========================================================================*)
CompAuth(p, e) ==
    IF FIX
      THEN \A m \in All \ {p} : e.st < kb[p][m]
      ELSE \/ e.st = pt[p]
           \/ /\ nbth[p] = 0
              /\ \A m \in All : (e.st =< clk[p][m]) \/ (clk[p][m] =< 0)

StartComp(p) ==
    /\ cmq[p] # {}
    /\ LET e == MinSt(cmq[p]) IN
        /\ ~ \E r \in running[p] : r.nj = e.nj
        /\ CompAuth(p, e)
        /\ cmq'     = [cmq EXCEPT ![p] = @ \ {e}]
        /\ running' = [running EXCEPT ![p] = @ \cup {e}]
        /\ pt'      = [pt EXCEPT ![p] = IF cmq[p] \ {e} = {} THEN -Abs(e.st) ELSE e.st]
        /\ nbth'    = [nbth EXCEPT ![p] = @ + 1]
    /\ UNCHANGED <<dly,cpq,et,clk,kb,chan,T,et0,bad>>

(*=========================================================================*)
(* EC body + CPC 2 -- result collector (fires or stays silent)              *)
(*=========================================================================*)
FinishComp(p) ==
    /\ running[p] # {}
    /\ \E e \in running[p] :
         /\ running' = [running EXCEPT ![p] = @ \ {e}]
         /\ nbth'    = [nbth EXCEPT ![p] = @ - 1]
         /\ \/ /\ Owner(e.nj) = p
               /\ LET cp == [ni |-> e.nj, st |-> e.st, crt |-> FALSE] IN
                    /\ cpq' = [cpq EXCEPT ![p] = @ \cup {cp}]
                    /\ et'  = [et EXCEPT ![p] = IF cpq[p] = {} THEN Abs(@) ELSE @]
            \/ /\ UNCHANGED <<cpq, et>>
    /\ UNCHANGED <<cmq,dly,pt,clk,kb,chan,T,et0,bad>>

(*=========================================================================*)
(* CMC 2 -- emission control. THE GUARD UNDER TEST.                         *)
(*=========================================================================*)
EmitAuth(p, e) ==
    IF FIX
      (* 2009: emission is authorized ONLY when every peer's channel clock has
         passed e.st. There is no "st_i = et_p", no "st_i <= pt_p" shortcut:
         those branches silently assume No-Straggler, which is the very thing
         that has to be established. This is de <= |te_j| + dmin, for all j. *)
      THEN /\ LocalClear(p, e.ni, e.st)
           /\ \A m \in All \ {p} : e.st < kb[p][m]
      (* 2006: four disjuncts, three of which presuppose No-Straggler *)
      ELSE \/ e.st = et[p]
           \/ e.crt
           \/ e.st =< pt[p]
           \/ /\ nbth[p] = 0
              /\ pt[p] < 0
              /\ \A m \in All \ {p} : (e.st =< clk[p][m]) \/ (clk[p][m] =< 0)

Emit(p) ==
    /\ cpq[p] # {}
    /\ \A q \in Procs \ {p} : CanSend(q)
    /\ LET e == MinSt(cpq[p]) IN
        /\ EmitAuth(p, e)
        (* ------------------ THE COMMIT POINT ------------------ *)
        /\ bad' = (bad \/ PendingFor(e.ni, e.st))
        /\ cpq' = [cpq EXCEPT ![p] = @ \ {e}]
        /\ et'  = [et EXCEPT ![p] = IF cpq[p] \ {e} = {} THEN -Abs(e.st) ELSE e.st]
        /\ LET outs  == { [nj |-> s.tgt, ni |-> e.ni, st |-> e.st + s.d] : s \in Post(e.ni) }
               valid == { o \in outs : o.st =< MaxT }
               local == { o \in valid : Owner(o.nj) = p }
               newet == IF cpq[p] \ {e} = {} THEN -Abs(e.st) ELSE e.st
               myclk == [k \in All |-> IF k = p THEN newet ELSE clk[p][k]]
               (* conservative: MyBound on the pre-state under-advertises,
                  which only ever makes peers stricter *)
               mybnd == MyBound(p)
           IN /\ cmq' = [cmq EXCEPT ![p] = @ \cup local]
              /\ chan' = [q \in Procs |->
                            IF q = p THEN chan[q]
                            ELSE LET toq == { o \in valid : Owner(o.nj) = q }
                                 IN Append(chan[q], Msg(p, toq, myclk, mybnd))]
              /\ clk' = [clk EXCEPT ![p] = myclk]
              /\ pt'  = [pt EXCEPT ![p] = IF local = {} THEN @ ELSE Abs(@)]
    /\ UNCHANGED <<dly,nbth,kb,T,et0,running>>

(*=========================================================================*)
Init ==
    /\ cmq  = [p \in Procs |-> {}]
    /\ dly  = [p \in Procs |-> {}]
    /\ cpq  = [p \in Procs |-> {}]
    /\ et   = [p \in Procs |-> 0]
    /\ pt   = [p \in Procs |-> 0]
    /\ nbth = [p \in Procs |-> 0]
    /\ clk  = [p \in Procs |-> [m \in All |-> 0]]
    /\ kb   = [p \in Procs |-> [m \in All |-> 0]]   \* known bound: 0 = "peer may emit at 0"
    /\ chan = [p \in Procs |-> << >>]
    /\ T    = 0
    /\ et0  = 0
    /\ running = [p \in Procs |-> {}]
    /\ bad  = FALSE

Next ==
    \/ Pr0Inject \/ Pr0Timeout
    \/ \E p \in Procs : NullSend(p)
    \/ \E p \in Procs : Receive(p)
    \/ \E p \in Procs : Promote(p)
    \/ \E p \in Procs : StartComp(p)
    \/ \E p \in Procs : FinishComp(p)
    \/ \E p \in Procs : Emit(p)

Spec == Init /\ [][Next]_vars

(*=========================================================================*)
TypeOK == /\ T \in 0..MaxT
          /\ \A p \in Procs : nbth[p] \in 0..4
          /\ bad \in BOOLEAN

Section4Inv == \A p \in Procs : /\ et0 >= Abs(et[p])
                                /\ et0 >= Abs(pt[p])

Exactness == ~bad

Constraint == /\ T =< MaxT
              /\ \A p \in Procs : Len(chan[p]) =< MaxChan
=============================================================================
