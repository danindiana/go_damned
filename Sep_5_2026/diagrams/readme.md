Plan: seven views — the conceptual thread, the engine architecture, the witness construction, the defect, what got fixed, the catch-22s, and open directions.Seven diagrams, `.dot` + `.png` (160 dpi) + `.svg` each, in `damned-witness/diagrams/`.

**01_argument_thread** — the arc from the brain as existence proof through the three gaps, the describability split, the TC⁰ baseline and its two escape routes, to the witness. Ends at what's still open.

**02_system_architecture** — engine versus additions. Existing Damned.go in blue, the witness files in green, verification in purple, with the soundness flag marked as blind.

**03_witness_construction** — the layers, the per-step tick schedule, and the no-leak difficulty with its fix. The bottom row states why the cancellation makes guard soundness and algebraic correctness the same property.

**04_defect_local_straggler** — the gate code, what it proves and what it doesn't, the two source placements, the four-neuron race, the fix and its verification.

**05_what_got_fixed** — three defects with symptoms and fixes, plus the result table and the note that failure rate rises with n.

**06_catch22s** — five tradeoffs: escaping TC⁰ costs parallelism, expressiveness costs verifiability twice over, latency inverts the defect, scale and lifelong learning are partly exclusive, and expressibility/selection governing all of them.

**07_future_directions** — the Tier 1 gate with its AND, three immediate cheap experiments, the Blackhole port checks, and the part nothing here touches.

Two nodes are marked **UNVERIFIED** in the diagrams themselves, per your standing rule: the TLA+ blind-spot hypothesis in 04 and the NoC per-pair FIFO guarantee in 07. Both are assertions I couldn't check from source. Nothing else in the set is estimated — every number traces to a run in this session.

Re-render any of them with `dot -Tpng -Gdpi=160 file.dot -o file.png`. Use the SVGs for slides or if you want to edit node text without touching DOT.

Two extensions worth considering: a composite poster tiling 03/04/05 as a single defect-report figure, and a diagram of the d_min sweep once you run it, since that curve is the one result that would go in a paper rather than a bug report.
