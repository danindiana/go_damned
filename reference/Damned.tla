---------------------------- MODULE Damned ----------------------------
(***************************************************************************)
(* An abstract model of the DAMNED 2006 emission-authorization mechanism   *)
(* (arXiv cs/0512018, section 4), reduced to the smallest system that can  *)
(* exhibit the straggler: two computing processors, integer time stamps.   *)
(*                                                                          *)
(* We model, per processor p:                                              *)
(*   et[p]   emission time  (signed: negative magnitude means CP queue     *)
(*           empty; we keep a separate Boolean cpEmpty[p] for clarity and   *)
(*           store et as a natural magnitude)                              *)
(*   pt[p]   processing time (magnitude)                                   *)
(*   nbth[p] active EC threads                                             *)
(*   clk[p]  p's last-known emission magnitude of the OTHER processor      *)
(*                                                                         *)
(* Each processor owns one neuron that will fire once, producing a CP      *)
(* event with a fixed stamp. Processor 1 owns an excitatory neuron whose   *)
(* forecast (stamp S1) can be cancelled by processor 2's inhibitory        *)
(* neuron (impact stamp S2 <= S1). The question TLC decides: can the       *)
(* emission guard authorize processor 1's CP event before it has accounted *)
(* for processor 2's cancelling event?                                     *)
(***************************************************************************)
EXTENDS Naturals, TLC

CONSTANTS S1,    \* stamp of processor 1's forecast excitatory spike
          S2     \* impact stamp of processor 2's inhibitory spike (S2 <= S1)

ASSUME S2 <= S1 /\ S1 \in Nat /\ S2 \in Nat

VARIABLES
    et,        \* [1..2 -> Nat]  emission-time magnitude
    cpEmpty,   \* [1..2 -> BOOLEAN]  CP queue empty?
    pt,        \* [1..2 -> Nat]  processing-time magnitude
    cmEmpty,   \* [1..2 -> BOOLEAN]  CM queue empty?
    nbth,      \* [1..2 -> Nat]
    clk,       \* clk[p] = p's known emission magnitude of the peer
    clkNeg,    \* clk sign: TRUE means the peer looked idle (et<=0) to p
    p2fired,   \* has processor 2 actually emitted its inhibitory spike?
    p2seen,    \* has processor 1 received the inhibition into its CM queue?
    committed  \* has processor 1 committed (emitted) its excitatory CP event?

vars == <<et,cpEmpty,pt,cmEmpty,nbth,clk,clkNeg,p2fired,p2seen,committed>>

Peer(p) == IF p = 1 THEN 2 ELSE 1

Init ==
    /\ et      = [q \in 1..2 |-> 0]
    /\ cpEmpty = [q \in 1..2 |-> TRUE]
    /\ pt      = [q \in 1..2 |-> 0]
    /\ cmEmpty = [q \in 1..2 |-> TRUE]
    /\ nbth    = [q \in 1..2 |-> 0]
    /\ clk     = [q \in 1..2 |-> 0]
    /\ clkNeg  = [q \in 1..2 |-> TRUE]  \* everyone starts looking idle
    /\ p2fired = FALSE
    /\ p2seen  = FALSE
    /\ committed = FALSE

(*-------------------------------------------------------------------------*)
(* Processor 2 fires its inhibitory neuron at impact stamp S2. This sets    *)
(* its own emission time and marks it non-idle, but processor 1 does not    *)
(* learn this until a message arrives (P1Receive).                          *)
(*-------------------------------------------------------------------------*)
P2Fire ==
    /\ ~p2fired
    /\ p2fired' = TRUE
    /\ et' = [et EXCEPT ![2] = S2]
    /\ cpEmpty' = [cpEmpty EXCEPT ![2] = FALSE]
    /\ UNCHANGED <<pt,cmEmpty,nbth,clk,clkNeg,p2seen,committed>>

(*-------------------------------------------------------------------------*)
(* Processor 1 receives processor 2's clock/event: it now knows the peer is *)
(* at emission S2 (non-idle) and the inhibitory CM event enters its queue.  *)
(*-------------------------------------------------------------------------*)
P1Receive ==
    /\ p2fired
    /\ ~p2seen
    /\ p2seen' = TRUE
    /\ clk'    = [clk EXCEPT ![1] = S2]
    /\ clkNeg' = [clkNeg EXCEPT ![1] = FALSE]  \* peer no longer looks idle
    /\ cmEmpty' = [cmEmpty EXCEPT ![1] = FALSE]
    /\ pt' = [pt EXCEPT ![1] = IF @ = 0 THEN 0 ELSE @]  \* reception: pt<-|pt|
    /\ UNCHANGED <<et,cpEmpty,nbth,clkNeg,p2fired,committed>>
    \* note: clkNeg' overrides the UNCHANGED via the EXCEPT above

(*-------------------------------------------------------------------------*)
(* Processor 1 produces its excitatory forecast: a CP event with stamp S1   *)
(* is queued (cpEmpty[1] := FALSE).                                         *)
(*-------------------------------------------------------------------------*)
P1Forecast ==
    /\ cpEmpty[1]
    /\ ~committed
    /\ cpEmpty' = [cpEmpty EXCEPT ![1] = FALSE]
    /\ UNCHANGED <<et,pt,cmEmpty,nbth,clk,clkNeg,p2fired,p2seen,committed>>

(*-------------------------------------------------------------------------*)
(* The section-4 emission-authorization predicate for processor 1's top CP  *)
(* event [N_i, S1, crt=false]. crt is false (the forecast is not certified  *)
(* because a cancelling input can still arrive). nbth[1]=0 here.            *)
(*                                                                          *)
(*   if S1 = et[1]                          -> authorized                   *)
(*   else if crt                            -> (crt=false, skip)            *)
(*   else if S1 <= pt[1]                     -> authorized                   *)
(*   else if nbth[1] = 0                     ->                             *)
(*       if pt[1]<0 (cmEmpty) and                                          *)
(*          (peer: S1 <= clk[1] or clkNeg[1])-> authorized                  *)
(*       else delayed                                                       *)
(*                                                                          *)
(* The peer disjunct  "S1 <= clk[1] OR clkNeg[1]"  is the section-4         *)
(* condition  "S1 <= et_m OR et_m <= 0".  clkNeg[1] is the et_m<=0 branch.  *)
(*-------------------------------------------------------------------------*)
EmitAuthorized ==
    \/ S1 = et[1]
    \/ S1 <= pt[1]
    \/ /\ nbth[1] = 0
       /\ cmEmpty[1]                       \* pt[1] < 0  <=>  CM queue empty
       /\ (S1 <= clk[1] \/ clkNeg[1])      \* the straggler-admitting disjunct

P1Emit ==
    /\ ~cpEmpty[1]
    /\ ~committed
    /\ EmitAuthorized
    /\ committed' = TRUE
    /\ et' = [et EXCEPT ![1] = S1]
    /\ cpEmpty' = [cpEmpty EXCEPT ![1] = TRUE]
    /\ UNCHANGED <<pt,cmEmpty,nbth,clk,clkNeg,p2fired,p2seen>>

Next == P2Fire \/ P1Receive \/ P1Forecast \/ P1Emit

Spec == Init /\ [][Next]_vars

(*=========================================================================*)
(* INVARIANTS                                                               *)
(*=========================================================================*)

TypeOK ==
    /\ et \in [1..2 -> Nat]
    /\ pt \in [1..2 -> Nat]
    /\ nbth \in [1..2 -> Nat]
    /\ committed \in BOOLEAN

(* Section-4 monotonicity-flavoured invariants that SHOULD hold.           *)
(* et_0 >= |et_p| is about the environment processor; here we check the    *)
(* intra-run structural ones we can express on 2 processors.               *)
MonoInv ==
    /\ (committed => et[1] = S1)          \* after emission et_p = st_i

(* The EXACTNESS safety property.                                          *)
(* A cancelling inhibitory event with stamp S2 <= S1 exists (p2fired).     *)
(* If processor 1 has committed its S1 spike WITHOUT having accounted for   *)
(* that event (~p2seen), the commit is unsound: in the real system the      *)
(* inhibition would have cancelled the forecast.                           *)
Exactness ==
    (committed /\ p2fired) => p2seen

=============================================================================
