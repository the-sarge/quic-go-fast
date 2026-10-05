# Single-family client sockets

## Why the fork binds them

On macOS, a dual-stack UDP socket (`AF_INET6` with `IPV6_V6ONLY=0`, bound to `[::]:0`) can be assigned a port that an IPv4 socket already holds on `127.0.0.1`. Datagrams to `127.0.0.1:port` then go to the IPv4 socket, and the dual-stack socket never receives them. With 1,000 such ports held, about 5–7% of dual-stack binds collided; an `AF_INET` socket bound with port 0 never did, in Go or in C. The mechanism, cross-host data and a FreeBSD control are in [#717's mechanism comment](https://github.com/the-sarge/quic-go-fast/issues/717#issuecomment-5982504875); [#717](https://github.com/the-sarge/quic-go-fast/issues/717) moved the affected test fixtures onto `127.0.0.1` sockets.

Upstream's client entry points created exactly that socket: `DialAddr` and `DialAddrEarly` bound `"udp"` on `0.0.0.0:0`, and `http3.Transport` without a custom `Dial` shared one `net.ListenUDP("udp", nil)` socket across every host. A client dialing a known address does not need a dual-stack socket, so [#720](https://github.com/the-sarge/quic-go-fast/issues/720) binds one of the target's family instead. Whether to offer this upstream is a separate decision after #720.

## Accepted contract

This is the user-approved one-PR contract for #720.

- `DialAddr` and `DialAddrEarly` resolve the address first, so a resolution failure creates no socket. They then bind `"udp4"` on `0.0.0.0:0` when the resolved IP is IPv4, including IPv4-mapped IPv6 (`To4() != nil`), and `"udp6"` on `[::]:0` otherwise, through the `listenUDPConn` seam. Transport setup, socket ownership and closing on error and on connection close are unchanged.
- `http3.Transport` with `Dial == nil` creates one `quic.Transport` per address family on the first dial to that family and dials each resolved target from the matching one. Resolution keeps the IPv4-preferring `forResolve` choice, so `localhost` still reaches `127.0.0.1` when it resolves to `[::1 127.0.0.1]`. `Close` closes clients, then the IPv4 transport and its socket, then the IPv6 transport and its socket, returning the first error as before; a closed `Transport` creates no new socket. The transports have their own mutex because `Close` holds the client mutex while it waits for in-flight dials. Behavior with a custom `Dial` is unchanged.
- There are no public API signature changes.

Non-goals are servers and the listen helpers (they need both families on one port), the #717 fixtures and #718 captures, the dual-stack `NewManagedPacketEndpointV1("udp", nil)` qualification cell, any workaround for a `0.0.0.0` holder, and proposing the change upstream.

Known effects beyond the goal: on macOS before 15, IPv4 client sockets now set DF, which was disabled for dual-stack sockets there. A socket creation failure in `http3.Transport` now fails only that dial instead of becoming the permanent initialization error. An empty-host target such as `DialAddr(ctx, ":443", …)` resolves to a nil IP and gets an IPv6 socket, which fails on a host without IPv6. A `Transport` that dials both families now uses two local ports.

## Representation and evidence

The code changes are shipped behavior; the new tests are verification aids, and this document is process metadata. Go's resolver owns the address representation, and the family choice is the single `To4() != nil` predicate in each owner, `listenUDPForAddr` in `client.go` and `Transport.transportFor` in `http3`. Family selection is universal over resolved addresses; collision avoidance is example-level on darwin because it rests on the kernel's IPv4 port-0 assignment. Contract closure is not triggered.

- `TestDialAddrSocketFamily` shows `udp4` for IPv4 and IPv4-mapped targets, `udp6` for IPv6, unspecified local addresses of the matching family, and no socket on resolution failure, for both dial functions. The resolution row left `TestDialAddrSetupFailureClosesSocket`, since no socket exists to close.
- `TestTransportDefaultDialPerFamily` gets responses from a `127.0.0.1` and a `[::1]` server through one `Transport`, shows a second IPv4 origin sharing the IPv4 socket, one socket per family, both released by `Close`, and `ErrTransportClosed` afterwards.
- `TestDialAddrAvoidsHeldLoopbackPorts` and `TestTransportDefaultDialAvoidsHeldLoopbackPorts` run only on darwin. Each holds 500 `127.0.0.1` ports and creates 300 client sockets through its owner, expecting about 9 collisions from dual-stack sockets and none from single-family ones. One guard-bypass mutation per owner, binding `"udp"` again, turned each red with 12 and 15 collisions.
- `TestHTTPClientTrace`, `TestHTTPDifferentOrigins` and `TestHandshakeAddrResolutionHelpers` keep exercising the default paths unchanged.
