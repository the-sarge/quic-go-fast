//go:build !boringcrypto

package handshake

// The non-Boring standard library AES/GCM constructors and x/crypto ChaCha
// implementations have immutable key state. Their nonce/mask wrappers do not.
const packetProtectionRuntime = true
