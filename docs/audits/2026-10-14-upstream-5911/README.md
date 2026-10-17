# Upstream #5911 overlap evidence

This folder supports the [proposed alignment record](../../adr/2026-10-14-upstream-5911-alignment.md).

- Fork analysis pin: `d3648918993fc8014d732e9a3a236e8df58a7047`.
- Record base: `9b9fd6437ed65422053eba3800cbf5ca2ed3d486`. The relevant source files are byte-identical to the analysis pin. The only intervening change in this area is `BBRSender.SetMaxDatagramSize`, which now calls a validating helper instead of constructing a fresh sender. Its behavior is unchanged, but a size of 0 still panics.
- Upstream: merge `99c743630f4a96a3c584ed64fb189db6ebd84f38` ([#5911](https://github.com/quic-go/quic-go/pull/5911)), parent `53a52d50e877f8d7522790da400a7a2776dbe5c4`. The fork's upstream base is `793f74d8e03368c5aded128af6f48d21dbb47f73`, 55 upstream commits behind the merge.
- [`upstream-5911.diff`](upstream-5911.diff) is the unmodified merge-commit diff, from `gh api repos/quic-go/quic-go/commits/99c74363… -H 'Accept: application/vnd.github.diff'`.

## Method

All evidence came from an isolated bare clone made with `git clone --bare --shared`, with the upstream commit fetched by SHA into that clone only. The primary checkout's refs, index and worktrees were not touched.

## Merge simulation (historical)

These results were recorded before the owner confirmed on 2026-10-14 that there will be no upstream sync. They are kept as history and no longer inform the decision.

Command for #5911 alone onto the fork: `git merge-tree --write-tree --name-only --messages --merge-base=53a52d50… d3648918 99c74363…`. Results:

- Content conflicts: `config.go` (1 block), `connection.go` (7 blocks), `connection_test.go` (4 blocks).
- Auto-merged with no markers: `config_test.go`, `interface.go` (the new godoc paragraph), `internal/congestion/cubic_sender.go` (the decrease panic removed), and `internal/congestion/cubic_sender_test.go`.
- In the merged tree, these fork-only reads of the raw field remain outside conflict markers. They read 0 once `populateConfig` keeps the field at 0:
  - `c.effectiveInitialPacketSize = protocol.ByteCount(c.config.InitialPacketSize)` in `preSetup`;
  - `c.sentPacketHandler.SetMaxDatagramSize(max(mtu, protocol.ByteCount(c.config.InitialPacketSize)))` in the ACK path.

Command for a full sync to the same upstream commit: `git merge-tree --write-tree --name-only --messages d3648918 99c74363…`. It produces content conflicts in 23 files: `.github/workflows/lint.yml`, `README.md`, `client_test.go`, `config.go`, `connection.go`, `connection_test.go`, `datagram_queue_test.go`, `http3/client.go`, `http3/error.go`, `http3/headers_test.go`, `http3/stream.go`, `integrationtests/self/handshake_drop_test.go`, `integrationtests/self/http_datagram_test.go`, `integrationtests/self/http_request_parsing_test.go`, `integrationtests/self/http_shutdown_test.go`, `integrationtests/self/http_stream_error_test.go`, `integrationtests/self/packetization_test.go`, `internal/ackhandler/sent_packet_handler.go`, `internal/protocol/params.go`, `internal/utils/rand_test.go`, `packet_packer.go`, `packet_packer_test.go` and `server_test.go`.

## Migration admission defect, reproduced on `d3648918`

This throwaway test was not committed to the fork. It was compiled and run only in the isolated clone's worktree, with `go test -count=1 -run TestScratchMigrationDatagramAdmission -v .`:

```go
package quic

import (
	"net"
	"testing"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// Scratch evidence only: does migration lower datagram admission with the packet size?
func TestScratchMigrationDatagramAdmission(t *testing.T) {
	tc := newConfiguredEmissionTestConnection(t, false, &Config{EnableDatagrams: true, DisablePathMTUDiscovery: true})
	c := tc.conn
	c.peerParams = &wire.TransportParameters{MaxDatagramFrameSize: 1456, MaxUDPPayloadSize: 1456}
	// As if path MTU discovery had already grown the old path to 1400 bytes.
	c.mtuDiscoverer = newMTUDiscoverer(c.rttStats, 1400, 1456, nil)
	c.maxPayloadSizeEstimate.Store(uint32(estimateMaxPayloadSize(1400)))
	t.Logf("before migration: maxPacketSize=%d admission=%d", c.maxPacketSize(), c.maxPayloadSizeEstimate.Load())

	raw := NewMockRawConn(gomock.NewController(t))
	raw.EXPECT().LocalAddr().Return(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242}).AnyTimes()
	raw.EXPECT().capabilities().Return(connCapabilities{}).AnyTimes()
	old := c.emission.queue
	go func() { _ = old.Run() }()
	c.switchToNewPath(&Transport{conn: raw}, monotime.Now())
	t.Logf("after migration:  maxPacketSize=%d admission=%d", c.maxPacketSize(), c.maxPayloadSizeEstimate.Load())

	payload := make([]byte, 1300) // cannot fit in a 1280-byte packet
	err := c.SendDatagram(payload)
	t.Logf("SendDatagram(1300 bytes) after migration -> err=%v", err)
	require.NoError(t, err, "fork admits a datagram that no longer fits")
	require.NotNil(t, c.datagramQueue.Peek())

	q := c.emission.queue.(*sendQueue)
	require.NoError(t, c.triggerSending(monotime.Now()).err)
	t.Logf("after packing: datagram still queued=%v, packets handed to socket queue=%d", c.datagramQueue.Peek() != nil, len(q.queue))
	require.Nil(t, c.datagramQueue.Peek(), "packer dropped the admitted datagram")
	c.emission.queue.Close()
}
```

Exact output:

```
=== RUN   TestScratchMigrationDatagramAdmission
    zz_scratch_migration_datagram_test.go:21: before migration: maxPacketSize=1400 admission=1363
    zz_scratch_migration_datagram_test.go:29: after migration:  maxPacketSize=1280 admission=1363
    zz_scratch_migration_datagram_test.go:33: SendDatagram(1300 bytes) after migration -> err=<nil>
    zz_scratch_migration_datagram_test.go:39: after packing: datagram still queued=false, packets handed to socket queue=0
--- PASS: TestScratchMigrationDatagramAdmission (0.00s)
PASS
ok  	github.com/quic-go/quic-go	0.161s
```

The packer discards a DATAGRAM frame that does not fit when the packet carries no ACK (`packet_packer.go:657`). The server path switch (`connection.go:1345`) has the same shape: it resets the finder and congestion state but never lowers `maxPayloadSizeEstimate`. Upstream's equivalent assertion is in the `testConnectionPathValidation` and `TestConnectionMigrationMTUDiscovery` hunks of the diff.

## Existing H1 tests and the explicit-floor contract

Every H1 regression configures an explicit size: `connection_handshake_mtu_test.go` uses 1452 and 1337, and `integrationtests/self/handshake_mtu_test.go` uses 1452. Under the proposed contract, each fallback case moves to an unset configuration, and explicit configurations gain a no-reduction case that ends in the documented handshake timeout.

## Probing after a path change with discovery disabled, reproduced on `d3648918`

This is a related defect, kept out of the decision record's scope. A second throwaway test, run the same way:

```go
package quic

import (
	"net"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/wire"
	"go.uber.org/mock/gomock"
)

// Scratch evidence only: does a path change start MTU probing although discovery is disabled?
func TestScratchMigrationProbeWithDiscoveryDisabled(t *testing.T) {
	tc := newConfiguredEmissionTestConnection(t, false, &Config{EnableDatagrams: true, DisablePathMTUDiscovery: true})
	c := tc.conn
	c.peerParams = &wire.TransportParameters{MaxDatagramFrameSize: 1456, MaxUDPPayloadSize: 1456}
	c.mtuDiscoverer = newMTUDiscoverer(c.rttStats, 1280, 1456, nil)
	now := monotime.Now()
	t.Logf("before migration: ShouldSendProbe(+1h)=%v", c.mtuDiscoverer.ShouldSendProbe(now.Add(time.Hour)))

	raw := NewMockRawConn(gomock.NewController(t))
	raw.EXPECT().LocalAddr().Return(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242}).AnyTimes()
	raw.EXPECT().capabilities().Return(connCapabilities{}).AnyTimes() // no DF either
	old := c.emission.queue
	go func() { _ = old.Run() }()
	c.switchToNewPath(&Transport{conn: raw}, now)
	later := now.Add(time.Hour)
	intent := c.prepareEmission(later)
	t.Logf("after migration: DisablePathMTUDiscovery=%v DF=false ShouldSendProbe(+1h)=%v prepareEmission selects MTU probe=%v",
		c.config.DisablePathMTUDiscovery, c.mtuDiscoverer.ShouldSendProbe(later), intent.mtu != nil)
	c.emission.queue.Close()
}
```

Exact output:

```
=== RUN   TestScratchMigrationProbeWithDiscoveryDisabled
    zz_scratch_migration_probe_test.go:20: before migration: ShouldSendProbe(+1h)=false
    zz_scratch_migration_probe_test.go:30: after migration: DisablePathMTUDiscovery=true DF=false ShouldSendProbe(+1h)=true prepareEmission selects MTU probe=true
--- PASS: TestScratchMigrationProbeWithDiscoveryDisabled (0.00s)
PASS
ok  	github.com/quic-go/quic-go	0.259s
```

`mtuFinder.Reset` (`mtu_discoverer.go:176`) sets `lastProbeTime`. The emission gate (`connection.go:2624`) checks neither `DisablePathMTUDiscovery` nor DF. Upstream fixed the equivalent in #5887.

