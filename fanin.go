package main

// buildFanin: the cancel scenario generalised to REMOTE excitation.
// Target T (theta=2) receives, all impacting at the SAME stamp:
//
//	+2 from a source on proc 2
//	+2 from a source on proc 3
//	-4 from an inhibitory source on proc 4
//
// Sequential semantics apply inhibition first at equal stamps, so T never fires.
// Their published `cancel` scenario has both excitations LOCAL to the target's
// processor; this variant makes them remote, which is the situation any dense
// network produces.
// buildFaninLocalInhib: same shape, but the INHIBITOR is co-located with the
// target and the excitations are remote -- the mirror image of `cancel`.
// The 2009 computation-authorization gate checks only PEER bounds; it never
// asks whether this processor's own un-emitted CP events could still deliver an
// impact at or before the stamp it is about to compute. So the target can
// integrate the remote +2,+2 and fire before the local inhibitory neuron has
// been committed. Sequential order applies -4 first, so the truth is no fire.
func buildFaninLocalInhib(P int) (*Network, []CMEvent, int) {
	net := NewNetwork(P)
	net.Add(0, 2, 1) // target, proc 1
	net.Add(1, 1, 2) // exc source, REMOTE
	net.Add(2, 1, P) // exc source, REMOTE
	net.Add(3, 1, 1) // inhib source, LOCAL to target
	net.Connect(1, 0, +2, 2)
	net.Connect(2, 0, +2, 2)
	net.Connect(3, 0, -4, 1)
	stim := []CMEvent{
		{Nj: 1, Ni: -1, St: 0, W: +1},
		{Nj: 2, Ni: -1, St: 0, W: +1},
		{Nj: 3, Ni: -1, St: 1, W: +1}, // fires at t=1, impacts at t=2
	}
	return net, stim, 4
}

func buildFanin(P int) (*Network, []CMEvent, int) {
	net := NewNetwork(P)
	net.Add(0, 2, 1) // target
	net.Add(1, 1, 2) // exc source, remote
	net.Add(2, 1, 3%P+1)
	net.Add(3, 1, P) // inhib source, remote
	net.Connect(1, 0, +2, 1)
	net.Connect(2, 0, +2, 1)
	net.Connect(3, 0, -4, 1)
	stim := []CMEvent{
		{Nj: 1, Ni: -1, St: 0, W: +1},
		{Nj: 2, Ni: -1, St: 0, W: +1},
		{Nj: 3, Ni: -1, St: 0, W: +1},
	}
	return net, stim, 4
}
