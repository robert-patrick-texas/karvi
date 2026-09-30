// Package planner builds the execution plan on the client: the address
// plan of every target, the draft plan and its header, the credential grants and bindings, and the credential package.
// Only internal/app imports it; the daemon validates what it produces and
// never runs it.
package planner
