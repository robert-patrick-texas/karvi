// Package scrapligov1 is karvi's scrapligo-v1 connection: x/crypto SSH with
// the host-key policy in the handshake, through scrapligo's transport
// wrapper, as a devsession.Stream. karvi's session
// layer speaks to the device; scrapligo's channel and network driver are not
// used. No other karvi package imports github.com/scrapli/scrapligo.
package scrapligov1

// Version is the exact upstream version this adapter is built and qualified
// against.
const Version = "v1.4.2"
