---
status: accepted
---

# Preserve upstream compatibility with optional datagram APIs

The fork preserves quic-go's existing public API and wire compatibility while allowing optional new APIs for datagram performance. This keeps adoption accessible to existing applications and allows interoperability with upstream peers, while leaving room for applications to opt into batching or different buffer-ownership contracts if measurements justify them.

Existing APIs retain their caller-visible contracts; a performance improvement must not silently impose new buffer-lifetime or ownership obligations on existing callers. Breaking existing APIs for performance is outside the accepted scope. Prohibiting all API additions was rejected because it would unnecessarily restrict optional improvements. The module path, release policy, and upstream synchronization cadence remain separate decisions.
