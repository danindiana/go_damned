# Examples & Visualizers

This folder contains example outputs, traces, and interactive tools for exploring the DAMNED distributed event-driven simulator.

## Contents

### `viz.html` — Interactive Trace Playback Visualizer

A self-contained, browser-based animator that visualizes a DAMNED simulation run as a spacetime diagram:

- **Processor lanes** (left): one horizontal lane per processor (Pr0, Pr1, ..., PrP)
- **Logical time** (horizontal): events progress left-to-right
- **Messages** (diagonals): spikes traversing between processors as diagonal lines
- **Straggler detection** (red flash): highlights events that violate the emission guard
- **Controls**: play/pause, scrub timeline, speed control, live event log

#### How to use:

1. **With pre-loaded samples** (no setup needed):
   ```bash
   open viz.html
   ```
   The visualizer ships with `trace_2006.json` and `trace_2009.json` built-in. Use the dropdown to switch between them.

2. **With your own trace**:
   ```bash
   # Generate a trace
   go run ../Damned.go -scenario=ring -guard=2006 -latency=1ms -trace=my_trace.json
   
   # Load it in viz.html
   open viz.html
   # Click "Load trace JSON" and select my_trace.json
   ```

#### Trace JSON Schema

```json
{
  "scenario": "cancel",
  "guard": "2009",
  "P": 2,
  "latency": "0s",
  "truth": [[0, 1], [1, 2], [2, 3]],
  "events": [
    {
      "kind": "send",        // send | recv | compute | commit
      "tns": 1234567890,     // wall-clock nanoseconds since run start
      "proc": 1,             // source processor (0 = Pr0)
      "dst": 2,              // destination (for send/recv)
      "neuron": 5,           // neuron ID
      "stamp": 3,            // logical spike timestamp
      "status": "ok",        // ok | straggler
      "wnj": 0,              // witness neuron (if straggler)
      "wst": 3,              // witness event timestamp
      "ww": -3               // witness weight (impact)
    },
    ...
  ]
}
```

### `trace_2006.json` — Example Cancellation Scenario with Buggy Guard

A pre-recorded trace from running the cancellation scenario with the **2006 emission guard** (which has the straggler hole). This trace demonstrates:

- Multiple events that violate correctness
- Red "straggler" markers for unsound commits
- Inhibitory spike arriving too late to cancel a premature excitatory firing

**Scenario**: Two processors, N0 (threshold 4) receives two +2 excitatory spikes and one -3 inhibitory spike, all impacting at t=3. The 2006 guard may commit N0's firing before the inhibitory spike arrives.

**Expected result** (sequential): N0 never fires (−3 cancels the +2+2 threshold crossing)

**Actual result** (2006 guard): N0 fires incorrectly (inhibitory straggler didn't block)

### `trace_2009.json` — Example Cancellation Scenario with Correct Guard

A pre-recorded trace from the **2009 CMB emission guard** (correct). This trace demonstrates:

- No straggler events
- All spikes match the sequential ground truth
- Bounds propagate before critical commitments

**Expected result** (sequential): N0 never fires  
**Actual result** (2009 guard): N0 never fires ✓ (sound)

### `damned.go` — Reference Copy

A copy of the main simulation engine (same as `../Damned.go`). Included here for convenience if you want to modify or run the engine separately within the examples directory.

## Quick Start

### 1. View Built-in Traces

```bash
cd examples
open viz.html
# or
firefox viz.html
# or
python3 -m http.server 8000  # then navigate to http://localhost:8000/viz.html
```

Click the dropdown in the visualizer to switch between `trace_2006.json` (buggy) and `trace_2009.json` (correct).

### 2. Generate Your Own Trace

```bash
cd ..
go run Damned.go -scenario=cancel -guard=2006 -latency=2ms -trace=examples/my_trace.json
go run Damned.go -scenario=ring -guard=2009 -reps=1 -trace=examples/ring_2009.json
```

Then load the JSON file in `examples/viz.html`.

### 3. Compare Guards Side-by-Side

Run the same scenario with both guards:

```bash
# Buggy guard
go run Damned.go -scenario=cancel -guard=2006 -latency=1ms -verbose

# Correct guard
go run Damned.go -scenario=cancel -guard=2009 -latency=1ms -verbose
```

Observe that the 2006 guard produces spurious commits, while the 2009 guard blocks until peer bounds advance.

## Understanding the Visualizer

### Lanes (Vertical)
- **Pr0**: Environment processor (stimulus injection, termination detection)
- **Pr1, Pr2, ...**: Compute processors

### Timeline (Horizontal)
Events flow left-to-right in wall-clock time. The scale is arbitrary; focus on the *order* of events.

### Message Diagonals
- **Blue diagonal**: Message sent from one processor to another
- **Red diagonal**: Message containing a straggler (witness to incorrect emission)

### Event Log (Bottom)
A live transcript of all events:
- `send Pr1→Pr2 N5@t=3` — processor 1 sends a spike to neuron 5 on processor 2, logical time 3
- `recv Pr1← 2 N5@t=3` — processor 1 receives the spike
- `compute N5@t=3` — neuron 5 integrates the impact at time 3
- `commit Pr1 N7@t=4 [ok]` — processor 1 commits neuron 7's firing at time 4 (sound)
- `commit Pr2 N0@t=3 [straggler]` — processor 2 commits incorrectly; a witness event explains why

### Play/Pause/Scrub
- **Play**: Animate events in real time (wall-clock)
- **Pause**: Freeze and inspect a single moment
- **Scrub**: Drag the slider to jump to any point in the trace
- **Speed**: Adjust playback speed (1x, 2x, 5x, 10x)

## Real Runtime Tracing (Go Tool)

For ultra-fine-grain visibility into goroutine scheduling, channel operations, and GC:

```bash
go run Damned.go -scenario=ring -guard=2006 -latency=500us -runtrace=run.trace
go tool trace run.trace
```

This opens the Go runtime tracer in your browser, showing:
- Every goroutine (Pr0, Pr1, ..., EC-threads, wireLoop-threads) as a timeline
- Channel sends and receives
- Processor goroutines blocked waiting for inbox/results
- Garbage collection pauses

Processor goroutines are tagged as tasks `Pr1`, `Pr2`, etc. Each neuron computation is a region `EC-Nx@t`, where `x` is the neuron ID and `t` is the logical time.

## Comparing 2006 vs 2009

The best way to see the bug:

```bash
# 2006: watch stragglers appear (especially with latency)
go run Damned.go -scenario=cancel -guard=2006 -latency=2ms -reps=50
# Output: "runs flagged unsound (straggler commit): 40/50"

# 2009: always sound
go run Damned.go -scenario=cancel -guard=2009 -latency=2ms -reps=50
# Output: "runs flagged unsound (straggler commit): 0/50"
```

Then load both traces in viz.html to see where the 2006 guard fails: inhibitory spike is in flight (red diagonal), but Pr1 commits before receiving it.

## File Manifest

| File | Size | Purpose |
|------|------|---------|
| `viz.html` | ~15 KB | Interactive trace visualizer (self-contained HTML + JS) |
| `trace_2006.json` | ~3.7 KB | Example trace: cancellation scenario, buggy guard (straggler detected) |
| `trace_2009.json` | ~3.5 KB | Example trace: cancellation scenario, correct guard (sound) |
| `damned.go` | ~31 KB | Reference copy of the engine |
| `README.md` | ~2 KB | Original README (from the archive) |
| `README_EXAMPLES.md` | (this file) | Guide to the examples folder |

## Troubleshooting

**viz.html shows "Loading..." forever**
- Make sure you have `trace_2006.json` or `trace_2009.json` in the same directory as `viz.html`
- If loading a custom trace, ensure it's valid JSON with the schema above

**Trace file is empty or incomplete**
- Re-run the engine with `-trace=` flag to generate a fresh trace
- Ensure the scenario completed (look for the ground-truth spike count at the top of the output)

**Can't open viz.html in browser**
- Try a local HTTP server: `python3 -m http.server 8000`, then `http://localhost:8000/examples/viz.html`
- Some browsers block `file://` URLs for security reasons

**Trace shows no straggler events in 2006 guard**
- Add latency and rerun: `-latency=1ms` or higher
- The straggler race is probabilistic; nondeterminism may hide it in a single run
- Run multiple times (`-reps=50`) to increase likelihood

## See Also

- [`../README.md`](../README.md) — Main documentation
- [`../diagrams/`](../diagrams/) — Architecture and correctness diagrams
- [`../paper/`](../paper/) — Source paper: Mouraud, Puzenat & Paugam-Moisy (2006/2009)

---

**Questions?** The visualizer is the best way to understand the algorithm. Load a trace, hit play, and watch the spikes race across processor lanes. The 2006 trace shows the bug; the 2009 trace shows correctness.
