package common

// CriticalExternalMsg marks messages that must bypass the normal lossy external
// message queue and go through the critical path instead.
type CriticalExternalMsg interface {
	MarkCriticalExternalMsg()
}

// NonEvictingCriticalExternalMsg marks critical messages that should drop
// themselves when the critical queue is full instead of evicting normal
// external messages.
type NonEvictingCriticalExternalMsg interface {
	CriticalExternalMsg
	MarkNonEvictingCriticalExternalMsg()
}
