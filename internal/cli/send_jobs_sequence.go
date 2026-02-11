package cli

func nextSendJobSequence(state *sendJobState) int64 {
	if state == nil {
		return 1
	}
	if state.NextSequence <= 0 {
		var maxSequence int64
		for _, job := range state.Jobs {
			if job.Sequence > maxSequence {
				maxSequence = job.Sequence
			}
		}
		state.NextSequence = maxSequence
	}
	state.NextSequence++
	return state.NextSequence
}
