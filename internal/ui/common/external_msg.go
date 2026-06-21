package common

// CriticalExternalMsg marks messages that must bypass the normal lossy external
// message queue and go through the critical path instead.
//
// Critical messages are non-evicting: when the critical queue is full they drop
// themselves rather than evicting a queued non-critical message. (An evicting
// tier used to exist, but no real message ever needed it, so the distinction was
// removed.)
type CriticalExternalMsg interface {
	MarkCriticalExternalMsg()
}
