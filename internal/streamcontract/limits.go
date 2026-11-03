package streamcontract

// Bound per-channel publishers as well as the existing per-stream viewer
// limits. The signaling layer and SFU must enforce the same resource limit.
const MaxChannelPublishers = 8
