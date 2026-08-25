// Package relay owns the self-hosted routing service.
//
// The M1 package is intentionally only a boundary marker. Transport, identity,
// ACL, and metadata persistence arrive in M2. Relay implementations must never
// persist request or response bodies.
package relay
