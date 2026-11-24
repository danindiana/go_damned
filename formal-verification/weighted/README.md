# DamnedWeighted: TLA+ model with real neurons

A model of the `Damned.go` engine that can tell a wrong spike from a right one. It adds what
`../DamnedFull2.tla` lacks: integer potentials, thresholds and signed weights; the engine's
event order (time, then inhibitory first); and invariants that compare committed spikes with
the correct spike train rather than checking for pending events.

Results and caveats: [`../../docs/VERIFICATION-2009L.md`](../../docs/VERIFICATION-2009L.md).

| File | What |
|---|---|
| `DamnedWeighted.tla.tmpl` | The model. Edit this, not the generated specs |
| `gen.py` | Writes one spec per network, with the correct spike train computed as in `sequentialEDS` |
| `run_tlc.sh` | Checks every spec under `2009` and `2009L`, each concurrent and one-at-a-time |
| `DW_*.tla`, `DW_*.cfg` | Generated specs for four named networks |
| `results/` | TLC output for the named networks, including counterexample traces |
| `random/summary.txt` | One line per random network and variant |

```bash
python3 gen.py                       # named networks
python3 gen.py --random 120 2026     # random networks -> random/
./run_tlc.sh /path/to/tla2tools.jar .
./run_tlc.sh /path/to/tla2tools.jar random
```

TLC: <https://github.com/tlaplus/tlaplus/releases> (tested with v1.8.0, Java 21).
