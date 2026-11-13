package quic

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/quicvarint"
)

var (
	// ErrMultipathHandshakeIncompleteV1 means multipath negotiation is still pending.
	ErrMultipathHandshakeIncompleteV1 = errors.New("quic: multipath negotiation is pending")
	// ErrMultipathNotNegotiatedV1 means the connection completed without multipath.
	ErrMultipathNotNegotiatedV1 = errors.New("quic: multipath was not negotiated")
	// ErrMultipathWrongModeV1 means a legacy method on a multipath path or connection,
	// or a multipath method on an ordinary migration path.
	ErrMultipathWrongModeV1 = errors.New("quic: path method of the other mode")
	// ErrMultipathLastPathV1 refuses the explicit abandonment of the last usable path.
	ErrMultipathLastPathV1 = errors.New("quic: cannot abandon the last usable path")
)

const (
	multipathPolicyPrimaryBackup = "primary-backup"
	// ownedMultipathWriteBudget is the native request duration on a socket the library
	// creates and owns.
	ownedMultipathWriteBudget = 100 * time.Millisecond
)

// multipathSelection is the configured request, copied with the Config that holds it.
type multipathSelection struct {
	enabled  bool
	maxPaths uint32
}

// ConfigureMultipathV1 selects multipath for the connections using this Config, before
// it reaches Dial, Listen or a GetConfigForClient return. maxPaths is 1 or 2, and counts
// active and validating paths including path zero; policy is "primary-backup". Both are
// ignored when disabling, which is the default. On error the Config is unchanged.
//
// Multipath is experimental and opt-in. Its one supported profile is draft-21 QUIC v1
// with Reno on Linux amd64, over a socket granted with [Transport.ConfigureMultipathIOV1].
// Elsewhere, or with a QUIC v2 version listed or another congestion controller, a public
// constructor refuses an enabled Config with [ErrMultipathUnsupportedV1]. A connection
// that negotiates QUIC v2, or whose peer doesn't negotiate multipath, stays ordinary.
func (c *Config) ConfigureMultipathV1(enabled bool, maxPaths uint32, policy string) error {
	if c == nil {
		return fmt.Errorf("%w: a nil Config", ErrMultipathInvalidConfigV1)
	}
	if !enabled {
		c.multipath = multipathSelection{}
		return nil
	}
	if maxPaths < 1 || maxPaths > 2 {
		return fmt.Errorf("%w: %d paths outside [1, 2]", ErrMultipathInvalidConfigV1, maxPaths)
	}
	if policy != multipathPolicyPrimaryBackup {
		return fmt.Errorf("%w: unknown policy %q", ErrMultipathInvalidConfigV1, policy)
	}
	c.multipath = multipathSelection{enabled: true, maxPaths: maxPaths}
	return nil
}

// MultipathEnabledV1 reports the selection made with [Config.ConfigureMultipathV1]. A
// helper that creates its own [Transport] reads it to grant that transport's socket.
func (c *Config) MultipathEnabledV1() bool {
	return c != nil && c.multipath.enabled
}

// ConfigureMultipathIOV1 grants the native endpoint exclusive write and deadline authority
// over the transport's exact unconnected *net.UDPConn, until a successful
// [Transport.WaitMultipathIOStoppedV1]. It must run before initialization. maxWriteDuration
// bounds one native request, and is between 1 ms and 1 s. It takes neither read deadlines
// nor close ownership.
//
// Off Linux amd64 it returns [ErrMultipathUnsupportedV1].
func (t *Transport) ConfigureMultipathIOV1(maxWriteDuration time.Duration) error {
	if !multipathProfileSupported {
		return errMultipathPlatform
	}
	return t.configureNativeIO(maxWriteDuration)
}

// WaitMultipathIOStoppedV1 waits for the join that returns write and deadline authority,
// after [Transport.Close]. Before Close it returns [ErrMultipathNotStoppingV1], and without
// a grant [ErrMultipathNotConfiguredV1]. Expiry of ctx only stops waiting. Success doesn't
// certify earlier writes or restore the former deadline.
func (t *Transport) WaitMultipathIOStoppedV1(ctx context.Context) error {
	return t.waitNativeIOStopped(ctx)
}

// multipathProfileSupported names the one qualified platform of the public profile. Every
// other build keeps refusing before any grant or advertisement.
const multipathProfileSupported = runtime.GOOS == "linux" && runtime.GOARCH == "amd64"

var errMultipathPlatform = fmt.Errorf("%w: multipath is qualified only on linux/amd64", ErrMultipathUnsupportedV1)

// admitMultipath is the one capability gate. Every constructor and every server-selected
// configuration passes it before advertisement or attachment, with the Config as the
// caller wrote it. A socket-owning helper gets its grant here; nothing retrofits one later.
func (t *Transport) admitMultipath(conf *Config) error {
	if !conf.MultipathEnabledV1() {
		return nil
	}
	if !multipathProfileSupported {
		return errMultipathPlatform
	}
	if conf.CongestionControlV1() != "reno" {
		return fmt.Errorf("%w: multipath requires reno", ErrMultipathUnsupportedV1)
	}
	if slices.ContainsFunc(conf.Versions, func(v Version) bool { return v != Version1 }) {
		return fmt.Errorf("%w: multipath requires QUIC v1", ErrMultipathUnsupportedV1)
	}
	if t.ConnectionIDGenerator != nil && t.ConnectionIDGenerator.ConnectionIDLen() == 0 {
		return fmt.Errorf("%w: multipath requires nonzero connection IDs", ErrMultipathInvalidConfigV1)
	}
	if err := t.nativeIOConflict(); err != nil {
		return err
	}
	switch {
	case t.responses != nil:
		return nil
	case t.createdConn:
		return t.grantOwnedSocket()
	case t.isSingleUse:
		return fmt.Errorf("%w: a caller's socket needs an explicit Transport with ConfigureMultipathIOV1", ErrMultipathUnsupportedV1)
	default:
		return fmt.Errorf("%w: the transport has no ConfigureMultipathIOV1 grant", ErrMultipathNotConfiguredV1)
	}
}

// grantOwnedSocket installs the default grant on a helper's private transport. A single-use
// transport would otherwise choose zero-length connection IDs.
func (t *Transport) grantOwnedSocket() error {
	if err := t.configureNativeIO(ownedMultipathWriteBudget); err != nil {
		return err
	}
	if t.ConnectionIDLength == 0 {
		t.ConnectionIDLength = protocol.DefaultConnectionIDLength
	}
	return nil
}

// MultipathPathStateV1 is an immutable observation of one path. Its numbers grant no
// scheduling or deadline authority.
type MultipathPathStateV1 struct {
	WireID          uint32
	TupleGeneration uint64
	Local           netip.AddrPort
	Remote          netip.AddrPort
	// Lifecycle is validating, live, suspect, retiring, retained or closed.
	Lifecycle string
	// LocalUse is active or backup.
	LocalUse string
	// PeerPreference is available, backup or unknown. It is advisory.
	PeerPreference string
	Primary        bool
	Validated      bool
	RTT            time.Duration
	// PMTU is the UDP payload limit; zero means not measured.
	PMTU uint32
	// LastOutcome is none, accepted, unsubmitted, uncertain or endpoint-fault. It is a
	// coarse category, and says nothing about safe replay.
	LastOutcome string
}

const (
	multipathPending int32 = iota
	multipathNegotiated
	multipathOrdinary
)

// multipathSurface is what callers reach off the connection goroutine. It exists only on
// a connection that advertised multipath.
type multipathSurface struct {
	mode    atomic.Int32
	changes chan struct{}

	mu       sync.Mutex
	handles  map[protocol.PathID]*Path
	snapshot []*Path // replaced, never edited; bounded by the path slots
	commands []*pathCommand
	sealed   bool // the connection ended, and runs no more commands
}

// pathCommand runs on the connection goroutine. Open and migrate keep their one local
// operation slot; a status or abandonment doesn't wait for one to finish.
type pathCommand struct {
	run  func(*pathCreditRegistry, monotime.Time) error
	done chan error
}

// noPathChanges serves a connection that can never have multipath paths.
var noPathChanges = make(chan struct{})

func newMultipathSurface() *multipathSurface {
	return &multipathSurface{changes: make(chan struct{}, 1), handles: make(map[protocol.PathID]*Path)}
}

// MultipathNegotiatedV1 reports whether the peer negotiated multipath. Before that is known
// it returns [ErrMultipathHandshakeIncompleteV1]. An ordinary peer, or a connection that
// didn't ask for multipath, reports false with no error.
func (c *Conn) MultipathNegotiatedV1() (bool, error) {
	if c.pathSurface == nil {
		return false, nil
	}
	switch c.pathSurface.mode.Load() {
	case multipathNegotiated:
		return true, nil
	case multipathOrdinary:
		return false, nil
	}
	if err := context.Cause(c.ctx); err != nil {
		return false, err
	}
	return false, ErrMultipathHandshakeIncompleteV1
}

// requireMultipath is the common check of a new path operation.
func (c *Conn) requireMultipath() error {
	negotiated, err := c.MultipathNegotiatedV1()
	if err != nil {
		return err
	}
	if !negotiated {
		return ErrMultipathNotNegotiatedV1
	}
	// Negotiation stays reported after the end, but nothing new starts.
	return context.Cause(c.ctx)
}

// legacyPathsBlocked is the mode check of the existing migration API. An unsupported peer
// leaves it available once that is known.
func (c *Conn) legacyPathsBlocked() error {
	negotiated, err := c.MultipathNegotiatedV1()
	if errors.Is(err, ErrMultipathHandshakeIncompleteV1) {
		return err
	}
	if negotiated {
		return ErrMultipathWrongModeV1
	}
	return nil
}

// OpenPathV1 opens a new path from t, which must hold a [Transport.ConfigureMultipathIOV1]
// grant, to remote, a server address the application already knows. It is client-only and
// returns after validation. Cancellation before an ID is committed releases only local
// reservations; afterward the connection retires the attempt, and the returned context
// error says nothing about the wire. It never closes or takes over t.
//
// A connection runs one local open or migration at a time, and a concurrent one returns
// [ErrMultipathBackpressureV1]. When the connection itself fails an admitted attempt, that
// attempt has already stopped counting by the time the error returns, unless a probe it
// submitted is still unresolved, in which case it counts until the probe resolves. So a retry
// can't be refused merely by the attempt it follows; path credit, connection IDs, target
// health, peer policy and a competing operation still decide its result. A returned context
// error promises none of this, since the connection's cleanup can still be running.
func (c *Conn) OpenPathV1(ctx context.Context, t *Transport, remote *net.UDPAddr) (*Path, error) {
	if err := c.requireMultipath(); err != nil {
		return nil, err
	}
	if t == nil {
		return nil, fmt.Errorf("%w: a nil target transport", ErrMultipathInvalidConfigV1)
	}
	if remote == nil {
		return nil, fmt.Errorf("%w: a nil remote address", ErrMultipathInvalidConfigV1)
	}
	// An address nothing can send to would consume an ID and then fail the connection.
	// The range check comes first, since AddrPort truncates an out-of-range port.
	if destination := remote.AddrPort(); remote.Port < 1 || remote.Port > 65535 || !destination.IsValid() || destination.Addr().Unmap().IsUnspecified() {
		return nil, fmt.Errorf("%w: remote address %s", ErrMultipathInvalidConfigV1, remote)
	}
	id, err := c.openPathPrivate(ctx, t, &net.UDPAddr{IP: slices.Clone(remote.IP), Port: remote.Port, Zone: remote.Zone})
	if err != nil {
		return nil, err
	}
	c.syncPathState()
	return c.pathSurface.handle(c, id), nil
}

// PathsV1 returns a snapshot of the connection's multipath paths, including path zero and
// peer-created paths. It is empty before negotiation and without support, which
// [Conn.MultipathNegotiatedV1] distinguishes.
func (c *Conn) PathsV1() []*Path {
	if c.pathSurface == nil {
		return []*Path{}
	}
	c.pathSurface.mu.Lock()
	defer c.pathSurface.mu.Unlock()
	return append([]*Path{}, c.pathSurface.snapshot...)
}

// PathChangesV1 returns a coalescing invalidation signal, not an event log: after a
// receive, take a new [Conn.PathsV1] snapshot. RTT updates alone don't signal it.
func (c *Conn) PathChangesV1() <-chan struct{} {
	if c.pathSurface == nil {
		return noPathChanges
	}
	return c.pathSurface.changes
}

// WaitTransportDetachedV1 waits until no live or validating path of this connection uses t,
// everything submitted through t has resolved, and t's routing can no longer destroy or send
// for this connection. It retires, migrates and closes nothing. Expiry of ctx only stops
// waiting. Success is what makes closing t safe for this connection's survivors.
func (c *Conn) WaitTransportDetachedV1(ctx context.Context, t *Transport) error {
	if t == nil {
		return fmt.Errorf("%w: a nil transport", ErrMultipathInvalidConfigV1)
	}
	if err := c.requireMultipath(); err != nil {
		return err
	}
	return c.waitTransportDetachedPrivate(ctx, t)
}

// PathIDV1 returns a multipath path's stable wire ID.
func (p *Path) PathIDV1() (uint32, error) {
	if p.conn == nil {
		return 0, ErrMultipathWrongModeV1
	}
	return uint32(p.wireID), nil
}

// StateV1 returns the path's latest observation, which outlives the path and the connection.
func (p *Path) StateV1() (MultipathPathStateV1, error) {
	if p.conn == nil {
		return MultipathPathStateV1{}, ErrMultipathWrongModeV1
	}
	return *p.state.Load(), nil
}

// MigrateV1 moves this client path to t under the same wire ID, keeping its remote address,
// packet numbers and keys. It consumes no path credit. It waits within ctx for a spare
// connection ID and for validation, with the old tuple untouched, and returns success only
// after cutover; cancellation after the connection commits still reports success. A failed
// attempt releases the connection's one local operation as [Conn.OpenPathV1] describes.
func (p *Path) MigrateV1(ctx context.Context, t *Transport) error {
	if p.conn == nil {
		return ErrMultipathWrongModeV1
	}
	if err := p.conn.requireMultipath(); err != nil {
		return err
	}
	if err := p.conn.migratePathPrivate(ctx, p.wireID, t); err != nil {
		return err
	}
	p.conn.syncPathState()
	return nil
}

// SetLocalUseV1 sets how the local scheduler uses the path: "active" or "backup".
func (p *Path) SetLocalUseV1(use string) error {
	if p.conn == nil {
		return ErrMultipathWrongModeV1
	}
	backup := use == "backup"
	if !backup && use != "active" {
		return fmt.Errorf("%w: unknown local use %q", ErrMultipathInvalidConfigV1, use)
	}
	return p.conn.runPathCommand(func(r *pathCreditRegistry, now monotime.Time) error {
		return r.setLocalUsePrivate(p.wireID, backup, now)
	})
}

// AdvertiseStatusV1 tells the peer how it should use the path: "available" or "backup". The
// peer needn't comply, and this doesn't promise zero incoming traffic.
func (p *Path) AdvertiseStatusV1(status string) error {
	if p.conn == nil {
		return ErrMultipathWrongModeV1
	}
	backup := status == "backup"
	if !backup && status != "available" {
		return fmt.Errorf("%w: unknown path status %q", ErrMultipathInvalidConfigV1, status)
	}
	return p.conn.runPathCommand(func(r *pathCreditRegistry, _ monotime.Time) error {
		return r.advertiseStatus(p.wireID, backup)
	})
}

// AbandonV1 retires the path with an application-chosen code. It is idempotent, and returns
// once the connection accepts the retirement, not after the peer acknowledges it. The last
// usable path returns [ErrMultipathLastPathV1] unless the caller closes the connection.
func (p *Path) AbandonV1(code uint64) error {
	if p.conn == nil {
		return ErrMultipathWrongModeV1
	}
	if code > quicvarint.Max {
		return fmt.Errorf("%w: abandon code %d", ErrMultipathInvalidConfigV1, code)
	}
	return p.conn.runPathCommand(func(r *pathCreditRegistry, _ monotime.Time) error {
		return r.abandonLocal(p.wireID, code)
	})
}

// runPathCommand hands one mutation to the connection goroutine. The published state
// reflects it when this returns.
func (c *Conn) runPathCommand(run func(*pathCreditRegistry, monotime.Time) error) error {
	surface := c.pathSurface
	command := &pathCommand{run: run, done: make(chan error, 1)}
	surface.mu.Lock()
	if surface.sealed {
		surface.mu.Unlock()
		<-c.ctx.Done()
		return context.Cause(c.ctx)
	}
	surface.commands = append(surface.commands, command)
	surface.mu.Unlock()
	c.scheduleSending()
	select {
	case err := <-command.done:
		return err
	case <-c.ctx.Done():
		return context.Cause(c.ctx)
	}
}

// syncPathState lets a caller's handle show what its completed operation did. A connection
// that ended has already published its last state.
func (c *Conn) syncPathState() {
	_ = c.runPathCommand(func(*pathCreditRegistry, monotime.Time) error { return nil })
}

func (p *pathCreditRegistry) serviceCommands(now monotime.Time) {
	surface := p.conn.pathSurface
	if surface == nil {
		return
	}
	surface.mu.Lock()
	commands := surface.commands
	surface.commands = nil
	surface.mu.Unlock()
	for _, command := range commands {
		err := command.run(p, now)
		p.reselect(now)
		p.publishPaths()
		command.done <- err
	}
}

func (p *pathCreditRegistry) advertiseStatus(id protocol.PathID, backup bool) error {
	if err := p.validateID(id); err != nil {
		return err
	}
	m := p.members[id]
	if m == nil || p.terminals[id] != nil {
		return ErrPathClosed
	}
	p.conn.queueControlFrame(&wire.PathStatusFrame{PathID: id, SequenceNumber: m.statusSeq, Backup: backup})
	m.statusSeq++
	p.conn.scheduleSending()
	return nil
}

// abandonLocal is the explicit retirement: it joins the one terminal owner, whose signal
// follows the submission fence, and cancels any attempt on the path.
func (p *pathCreditRegistry) abandonLocal(id protocol.PathID, code uint64) error {
	if err := p.validateID(id); err != nil {
		return err
	}
	if p.released(id) || p.terminals[id] != nil {
		return nil
	}
	if p.isLastPath(id) {
		return ErrMultipathLastPathV1
	}
	p.decline(id, code, ErrPathClosed)
	return nil
}

// handle returns the one Path of a wire ID. Only live or retained IDs stay in the map.
func (s *multipathSurface) handle(c *Conn, id protocol.PathID) *Path {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handleLocked(c, id)
}

func (s *multipathSurface) handleLocked(c *Conn, id protocol.PathID) *Path {
	handle := s.handles[id]
	if handle == nil {
		handle = &Path{conn: c, wireID: id}
		handle.state.Store(&MultipathPathStateV1{WireID: uint32(id), Lifecycle: "closed", LocalUse: "active", PeerPreference: "unknown", LastOutcome: "none"})
		s.handles[id] = handle
	}
	return handle
}

// publishPaths replaces the observations that changed, on the connection goroutine. A handle
// that leaves the snapshot keeps its last state, with the lifecycle closed.
func (p *pathCreditRegistry) publishPaths() {
	surface := p.conn.pathSurface
	if surface == nil || !p.hasAdmission() {
		return
	}
	var buf [4]*pathMember
	members := p.orderedMembers(buf[:0])
	surface.mu.Lock()
	defer surface.mu.Unlock()
	changed := len(members) != len(surface.snapshot)
	current := make([]*Path, 0, len(members))
	for _, m := range members {
		handle := surface.handleLocked(p.conn, m.id)
		current = append(current, handle)
		state := p.observation(m)
		last := handle.state.Load()
		if !state.Remote.IsValid() {
			state.Remote, state.Local = last.Remote, last.Local // a failed attempt's tuple
		}
		if state != *last {
			handle.state.Store(&state)
			lastWithRTT := *last
			lastWithRTT.RTT = state.RTT
			changed = changed || state != lastWithRTT
		}
	}
	for id, handle := range surface.handles {
		if !slices.Contains(current, handle) {
			closed := *handle.state.Load()
			closed.Lifecycle, closed.Primary = "closed", false
			handle.state.Store(&closed)
			delete(surface.handles, id)
			changed = true
		}
	}
	if !changed {
		return
	}
	surface.snapshot = current
	select {
	case surface.changes <- struct{}{}:
	default:
	}
}

// publishClosed is the connection's last publication, with what ended it visible. The
// final snapshot stays readable, and a caller still waiting leaves on the connection's end.
func (p *pathCreditRegistry) publishClosed() {
	surface := p.conn.pathSurface
	if surface == nil {
		return
	}
	surface.mu.Lock()
	defer surface.mu.Unlock()
	surface.sealed, surface.commands = true, nil
	for id, handle := range surface.handles {
		closed := *handle.state.Load()
		if m := p.members[id]; m != nil {
			last := closed
			closed = p.observation(m)
			if !closed.Remote.IsValid() {
				closed.Remote, closed.Local = last.Remote, last.Local
			}
		}
		closed.Lifecycle, closed.Primary = "closed", false
		handle.state.Store(&closed)
	}
	select {
	case surface.changes <- struct{}{}:
	default:
	}
}

func (p *pathCreditRegistry) observation(m *pathMember) MultipathPathStateV1 {
	state := MultipathPathStateV1{
		WireID:          uint32(m.id),
		TupleGeneration: m.tuple,
		Lifecycle:       "live",
		LocalUse:        "active",
		PeerPreference:  "unknown",
		Primary:         p.primary == m.id,
		Validated:       m.live,
		PMTU:            p.pmtuObservation(m.id),
		LastOutcome:     m.lastOutcome,
	}
	// A validating path's tuple is its candidate. A migration's stays the established one
	// until commit, since this path's established tuple is what still carries it.
	remote, info, local := m.addr, m.info, m.transport
	if !m.live && m.candidate != nil {
		remote, info, local = m.candidate.addr, m.candidate.info, m.candidate.transport
	}
	state.Remote = addrPortOf(remote)
	if local == nil {
		local = p.origin
	}
	if local != nil && local.Conn != nil {
		state.Local = addrPortOf(local.Conn.LocalAddr())
		// A wildcard socket's real source is what the packets carry.
		if info.addr.IsValid() {
			state.Local = netip.AddrPortFrom(info.addr.Unmap(), state.Local.Port())
		}
	}
	if local != nil && endpointFaulted(local) {
		state.LastOutcome = "endpoint-fault"
	}
	if rtt := p.rttOf(m.id); rtt != nil {
		state.RTT = rtt.SmoothedRTT()
	}
	if m.backup {
		state.LocalUse = "backup"
	}
	if m.peerSet {
		state.PeerPreference = "available"
		if m.peerBackup {
			state.PeerPreference = "backup"
		}
	}
	if state.LastOutcome == "" {
		state.LastOutcome = "none"
	}
	terminal := p.terminals[m.id]
	switch {
	case terminal != nil && terminal.signaled:
		state.Lifecycle = "retained"
	case terminal != nil:
		state.Lifecycle = "retiring"
	case !m.live:
		state.Lifecycle = "validating"
	case m.suspect:
		state.Lifecycle = "suspect"
	}
	return state
}

func addrPortOf(addr net.Addr) netip.AddrPort {
	if udp, ok := addr.(*net.UDPAddr); ok {
		ap := udp.AddrPort()
		return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
	}
	return netip.AddrPort{}
}
