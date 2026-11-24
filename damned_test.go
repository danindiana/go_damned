package main

import "testing"

// These tests check the claims the README makes about guard 2009L: zero
// spike-train mismatches against the sequential reference. They cannot show
// that the unsound guards fail, because whether a race fires depends on
// goroutine scheduling; run the binary for that.

func mismatches(t *testing.T, net *Network, stim []CMEvent, tMax int, guard string, reps int) int {
	t.Helper()
	truth := sequentialEDS(net, stim, tMax)
	n := 0
	for r := 0; r < reps; r++ {
		got, _, _ := runOnce(net, stim, Config{Guard: guard, TMax: tMax})
		if !equalPairs(got, truth) {
			n++
		}
	}
	return n
}

func TestCancel2009L(t *testing.T) {
	net, stim := buildCancellation()
	if m := mismatches(t, net, stim, 12, "2009L", 200); m != 0 {
		t.Fatalf("cancel, 2009L: %d/200 runs diverged from the sequential reference", m)
	}
}

func TestRing2009L(t *testing.T) {
	net, stim, tMax := buildRing()
	if m := mismatches(t, net, stim, tMax, "2009L", 200); m != 0 {
		t.Fatalf("ring, 2009L: %d/200 runs diverged", m)
	}
}

func TestFaninLocal2009L(t *testing.T) {
	for _, P := range []int{2, 3, 4} {
		net, stim, tMax := buildFaninLocalInhib(P)
		if m := mismatches(t, net, stim, tMax, "2009L", 300); m != 0 {
			t.Fatalf("fanin-local P=%d, 2009L: %d/300 runs diverged", P, m)
		}
	}
}

func TestFaninLocalTruth(t *testing.T) {
	// The sequential reference applies the -4 first, so the target (neuron 0)
	// never fires; only the three stimulated sources do.
	net, stim, tMax := buildFaninLocalInhib(2)
	truth := sequentialEDS(net, stim, tMax)
	for _, sp := range truth {
		if sp[1] == 0 {
			t.Fatalf("target fired in the sequential reference: %v", truth)
		}
	}
}

func TestWordWitnessS5(t *testing.T) {
	res := runWordWitness("s5", 8, 3, 2, 5, Config{Guard: "2009L"})
	if !res.EncodingOK {
		t.Fatal("S5 encoding check failed: the spiking network does not compute the group product")
	}
	if res.Neurons != 364 || res.Synapses != 1080 {
		t.Fatalf("network size %d neurons / %d synapses, want 364 / 1080", res.Neurons, res.Synapses)
	}
	if res.TrainMism != 0 || res.AnswerWrong != 0 {
		t.Fatalf("2009L: %d train mismatches, %d wrong products", res.TrainMism, res.AnswerWrong)
	}
}

func TestWordSizeConstantInN(t *testing.T) {
	for _, n := range []int{4, 16, 32} {
		res := runWordWitness("s5", n, 3, 1, 0, Config{Guard: "2009L"})
		if !res.EncodingOK || res.Neurons != 364 || res.Synapses != 1080 {
			t.Fatalf("n=%d: encoding=%v neurons=%d synapses=%d", n, res.EncodingOK, res.Neurons, res.Synapses)
		}
	}
}
