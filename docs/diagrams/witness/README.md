# Witness and defect diagrams (2026-09-05)

| File | Shows |
|---|---|
| `01_argument_thread.png` | The research argument that led to the witness, from the brain as an existence proof to the TC⁰ baseline |
| `02_system_architecture.png` | The engine (blue) against the witness additions (green) and verification (purple) |
| `03_witness_construction.png` | The S5 word-problem network: layers, per-step tick schedule, and how the no-leak neuron is handled |
| `04_defect_local_straggler.png` | The 2009 computation gate, the four-neuron race, and the `2009L` fix |
| `05_what_got_fixed.png` | The three defects with symptoms and fixes |
| `06_catch22s.png` | Five trade-offs, including latency masking the 2009 defect |
| `07_future_directions.png` | Open experiments and checks for a Blackhole port |

Two boxes in these diagrams were marked **UNVERIFIED** when drawn:

- **04, the TLA+ blind spot.** Since answered: the model cannot detect this class. See
  [`../../DEFECT-local-straggler.md`](../../DEFECT-local-straggler.md), section 7.
- **07, per-pair FIFO ordering on the Blackhole network-on-chip.** Still unverified. The
  algorithm depends on it (see the main README, "Per-channel FIFO is non-negotiable").

The numbers in 04 and 05 are from the 2026-09-05 runs. Failure rates under the `2009` guard
vary between runs; see [`../../results/2026-11-24-sweep.txt`](../../results/2026-11-24-sweep.txt)
for a later rerun. The `2009L` result (zero failures) has held in every run.
