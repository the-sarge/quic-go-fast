//go:build boringcrypto

package handshake

// BoringCrypto returns different concrete primitives. Their opaque native
// contexts are outside the source-proven immutable capability and stay synchronous.
const packetProtectionRuntime = false
