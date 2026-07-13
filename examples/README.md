# DAMNED — a living slice, in Go, with two visualizers

Faithful concurrent reconstruction of the DAMNED spiking-neural-network engine
(Mouraud, Puzenat & Paugam-Moisy, 2006/2009). One goroutine per processor,
controllers in a select loop, one EC-thread goroutine per neuron computation,
MPI links as FIFO channels. No barrier.

## Run the engine

    go run damned.go                              # correct engine (2009 guard)
    go run damned.go -guard=2006 -latency=2ms      # watch exactness break
    go run damned.go -guard=2006 -latency=2ms -verbose   # narrate each commit
    go run damned.go -scenario=ring -reps=100
    go run -race damned.go -reps=10 -latency=500us  # clean under the race detector

Flags: -guard {2009|2006}, -scenario {cancel|ring}, -latency (e.g. 2ms),
-reps N, -verbose, -trace FILE, -runtrace FILE.

## Visualizer 1 — logical spacetime playback (viz.html)

A self-contained page that animates a run as processor lanes over logical time,
with messages as diagonals crossing the wire and the straggler flashing red.

    go run damned.go -guard=2006 -latency=2ms -trace=trace.json
    # then open viz.html in a browser and Load trace JSON (or use the samples)

viz.html ships with the 2006 and 2009 cancellation traces built in, so it works
with no file loaded. Play / pause / scrub / speed controls; a live event log.

The JSON schema (array under "events"):
  kind   : send | recv | compute | commit
  tns    : wall-clock nanoseconds since run start
  proc   : processor raising the event (0 = Pr0)
  dst    : destination processor (send/recv)
  neuron : neuron id ; stamp : logical spike stamp
  status : ok | straggler ; wnj/wst/ww : the cancelling-event witness

## Visualizer 2 — the real Go runtime timeline (go tool trace)

This is the genuine execution: every goroutine (each processor, each EC-thread,
each wire), every channel send/receive, every block and unblock, on the runtime's
own timeline. Processor goroutines are tagged as tasks "Pr1".."PrN"; each neuron
computation is a region "EC-Nx@t".

    go run damned.go -guard=2006 -scenario=ring -latency=200us -runtrace=run.trace
    go tool trace run.trace          # opens the trace viewer in your browser

In the viewer, look at "Goroutine analysis" and the "User-defined tasks/regions"
views to see the EC-threads spawning and the processors blocking on channels.
