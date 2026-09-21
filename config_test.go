package quic

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/quicvarint"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigHandshakeIdleTimeout(t *testing.T) {
	c := &Config{HandshakeIdleTimeout: time.Second * 11 / 2}
	require.Equal(t, 11*time.Second, c.handshakeTimeout())
}

func configWithNonZeroNonFunctionFields(t *testing.T) *Config {
	t.Helper()
	c := &Config{}
	v := reflect.ValueOf(c).Elem()

	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		f := v.Field(i)
		if !f.CanSet() {
			// unexported field; not cloned.
			continue
		}

		switch fn := typ.Field(i).Name; fn {
		case "GetConfigForClient", "RequireAddressValidation", "GetLogWriter", "AllowConnectionWindowIncrease", "Tracer":
			// Can't compare functions.
		case "Versions":
			f.Set(reflect.ValueOf([]Version{1, 2, 3}))
		case "ConnectionIDLength":
			f.Set(reflect.ValueOf(8))
		case "ConnectionIDGenerator":
			f.Set(reflect.ValueOf(&protocol.DefaultConnectionIDGenerator{ConnLen: protocol.DefaultConnectionIDLength}))
		case "HandshakeIdleTimeout":
			f.Set(reflect.ValueOf(time.Second))
		case "MaxIdleTimeout":
			f.Set(reflect.ValueOf(time.Hour))
		case "TokenStore":
			f.Set(reflect.ValueOf(NewLRUTokenStore(2, 3)))
		case "InitialStreamReceiveWindow":
			f.Set(reflect.ValueOf(uint64(1234)))
		case "MaxStreamReceiveWindow":
			f.Set(reflect.ValueOf(uint64(9)))
		case "InitialConnectionReceiveWindow":
			f.Set(reflect.ValueOf(uint64(4321)))
		case "MaxConnectionReceiveWindow":
			f.Set(reflect.ValueOf(uint64(10)))
		case "MaxIncomingStreams":
			f.Set(reflect.ValueOf(int64(11)))
		case "MaxIncomingUniStreams":
			f.Set(reflect.ValueOf(int64(12)))
		case "StatelessResetKey":
			f.Set(reflect.ValueOf(&StatelessResetKey{1, 2, 3, 4}))
		case "KeepAlivePeriod":
			f.Set(reflect.ValueOf(time.Second))
		case "EnableDatagrams":
			f.Set(reflect.ValueOf(true))
		case "DisableVersionNegotiationPackets":
			f.Set(reflect.ValueOf(true))
		case "InitialPacketSize":
			f.Set(reflect.ValueOf(uint16(1350)))
		case "DisablePathMTUDiscovery":
			f.Set(reflect.ValueOf(true))
		case "Allow0RTT":
			f.Set(reflect.ValueOf(true))
		case "EnableStreamResetPartialDelivery":
			f.Set(reflect.ValueOf(true))
		default:
			t.Fatalf("all fields must be accounted for, but saw unknown field %q", fn)
		}
	}
	return c
}

func TestConfigClone(t *testing.T) {
	t.Run("function fields", func(t *testing.T) {
		var calledAllowConnectionWindowIncrease, calledTracer bool
		c1 := &Config{
			GetConfigForClient:            func(info *ClientInfo) (*Config, error) { return nil, assert.AnError },
			AllowConnectionWindowIncrease: func(*Conn, uint64) bool { calledAllowConnectionWindowIncrease = true; return true },
			Tracer: func(context.Context, bool, ConnectionID) qlogwriter.Trace {
				calledTracer = true
				return nil
			},
		}
		c2 := c1.Clone()
		c2.AllowConnectionWindowIncrease(nil, 1234)
		require.True(t, calledAllowConnectionWindowIncrease)
		_, err := c2.GetConfigForClient(&ClientInfo{})
		require.ErrorIs(t, err, assert.AnError)
		c2.Tracer(context.Background(), true, protocol.ConnectionID{})
		require.True(t, calledTracer)
	})

	t.Run("non-function fields", func(t *testing.T) {
		c := configWithNonZeroNonFunctionFields(t)
		require.Equal(t, c, c.Clone())
	})

	t.Run("returns a copy", func(t *testing.T) {
		c1 := &Config{MaxIncomingStreams: 100}
		c2 := c1.Clone()
		c2.MaxIncomingStreams = 200
		require.EqualValues(t, 100, c1.MaxIncomingStreams)
	})
}

func TestConfigDefaultValues(t *testing.T) {
	// if set, the values should be copied
	c := configWithNonZeroNonFunctionFields(t)
	require.Equal(t, c, populateConfig(c))

	// if not set, some fields use default values
	c = populateConfig(&Config{})
	require.Equal(t, protocol.SupportedVersions, c.Versions)
	require.Equal(t, protocol.DefaultHandshakeIdleTimeout, c.HandshakeIdleTimeout)
	require.Equal(t, protocol.DefaultIdleTimeout, c.MaxIdleTimeout)
	require.EqualValues(t, protocol.DefaultInitialMaxStreamData, c.InitialStreamReceiveWindow)
	require.EqualValues(t, protocol.DefaultMaxReceiveStreamFlowControlWindow, c.MaxStreamReceiveWindow)
	require.EqualValues(t, protocol.DefaultInitialMaxData, c.InitialConnectionReceiveWindow)
	require.EqualValues(t, protocol.DefaultMaxReceiveConnectionFlowControlWindow, c.MaxConnectionReceiveWindow)
	require.EqualValues(t, protocol.DefaultMaxIncomingStreams, c.MaxIncomingStreams)
	require.EqualValues(t, protocol.DefaultMaxIncomingUniStreams, c.MaxIncomingUniStreams)
	require.Equal(t, uint16(protocol.InitialPacketSize), c.InitialPacketSize)
	require.False(t, c.DisablePathMTUDiscovery)
	require.Nil(t, c.GetConfigForClient)
}

func TestConfigZeroLimits(t *testing.T) {
	config := &Config{
		MaxIncomingStreams:    -1,
		MaxIncomingUniStreams: -1,
	}
	c := populateConfig(config)
	require.Zero(t, c.MaxIncomingStreams)
	require.Zero(t, c.MaxIncomingUniStreams)
}

func TestConfigPreparationForTransport(t *testing.T) {
	t.Run("nil uses defaults", func(t *testing.T) {
		prepared, err := prepareConfig(nil)
		require.NoError(t, err)
		require.Equal(t, protocol.SupportedVersions, prepared.Versions)
		require.EqualValues(t, protocol.DefaultInitialMaxStreamData, prepared.InitialStreamReceiveWindow)
		require.EqualValues(t, protocol.DefaultInitialMaxData, prepared.InitialConnectionReceiveWindow)
	})

	t.Run("prepares effective values without widening caller mutation", func(t *testing.T) {
		versions := []Version{protocol.SupportedVersions[0]}
		tokenStore := NewLRUTokenStore(2, 3)
		conf := &Config{
			Versions:                       versions,
			TokenStore:                     tokenStore,
			InitialStreamReceiveWindow:     quicvarint.Max + 1,
			MaxStreamReceiveWindow:         0,
			InitialConnectionReceiveWindow: quicvarint.Max + 2,
			MaxConnectionReceiveWindow:     0,
			MaxIncomingStreams:             -1,
			MaxIncomingUniStreams:          0,
			InitialPacketSize:              0,
		}

		prepared, err := prepareConfig(conf)
		require.NoError(t, err)

		require.Equal(t, uint64(quicvarint.Max), prepared.InitialStreamReceiveWindow)
		require.EqualValues(t, protocol.DefaultMaxReceiveStreamFlowControlWindow, prepared.MaxStreamReceiveWindow)
		require.Equal(t, uint64(quicvarint.Max), prepared.InitialConnectionReceiveWindow)
		require.EqualValues(t, protocol.DefaultMaxReceiveConnectionFlowControlWindow, prepared.MaxConnectionReceiveWindow)
		require.Zero(t, prepared.MaxIncomingStreams)
		require.EqualValues(t, protocol.DefaultMaxIncomingUniStreams, prepared.MaxIncomingUniStreams)
		require.Equal(t, uint16(protocol.InitialPacketSize), prepared.InitialPacketSize)

		require.Equal(t, uint64(quicvarint.Max+1), conf.InitialStreamReceiveWindow)
		require.Zero(t, conf.MaxStreamReceiveWindow)
		require.Equal(t, uint64(quicvarint.Max+2), conf.InitialConnectionReceiveWindow)
		require.Zero(t, conf.MaxConnectionReceiveWindow)
		require.Equal(t, int64(-1), conf.MaxIncomingStreams)
		require.Zero(t, conf.MaxIncomingUniStreams)
		require.Zero(t, conf.InitialPacketSize)
		require.Same(t, &versions[0], &prepared.Versions[0])
		require.Same(t, tokenStore, prepared.TokenStore)
	})

	t.Run("preserves legacy clipping before version errors", func(t *testing.T) {
		conf := &Config{
			Versions:                       []Version{0x1234},
			InitialStreamReceiveWindow:     quicvarint.Max + 1,
			MaxStreamReceiveWindow:         quicvarint.Max + 1,
			InitialConnectionReceiveWindow: quicvarint.Max + 2,
			MaxConnectionReceiveWindow:     quicvarint.Max + 2,
			MaxIncomingStreams:             1<<60 + 1,
			MaxIncomingUniStreams:          1<<60 + 2,
			InitialPacketSize:              1,
		}

		prepared, err := prepareConfig(conf)
		require.Nil(t, prepared)
		require.ErrorContains(t, err, "invalid QUIC version: 0x1234")
		require.Equal(t, uint64(quicvarint.Max+1), conf.InitialStreamReceiveWindow)
		require.Equal(t, uint64(quicvarint.Max), conf.MaxStreamReceiveWindow)
		require.Equal(t, uint64(quicvarint.Max+2), conf.InitialConnectionReceiveWindow)
		require.Equal(t, uint64(quicvarint.Max), conf.MaxConnectionReceiveWindow)
		require.Equal(t, int64(1<<60), conf.MaxIncomingStreams)
		require.Equal(t, int64(1<<60), conf.MaxIncomingUniStreams)
		require.Equal(t, uint16(protocol.MinInitialPacketSize), conf.InitialPacketSize)
	})
}

func TestConfigPreparationForClient(t *testing.T) {
	nestedCallbackCalls := 0
	conf := &Config{
		GetConfigForClient: func(*ClientInfo) (*Config, error) {
			nestedCallbackCalls++
			return nil, nil
		},
		Versions:                       []Version{0x1234},
		InitialStreamReceiveWindow:     quicvarint.Max + 1,
		MaxStreamReceiveWindow:         quicvarint.Max + 2,
		InitialConnectionReceiveWindow: quicvarint.Max + 3,
		MaxConnectionReceiveWindow:     quicvarint.Max + 4,
		MaxIncomingStreams:             1<<60 + 1,
		MaxIncomingUniStreams:          1<<60 + 2,
		InitialPacketSize:              protocol.MaxPacketBufferSize + 1,
	}
	prepared := prepareConfigForClient(conf)

	require.Equal(t, []Version{0x1234}, conf.Versions)
	require.Equal(t, uint64(quicvarint.Max+1), conf.InitialStreamReceiveWindow)
	require.Equal(t, uint64(quicvarint.Max+2), conf.MaxStreamReceiveWindow)
	require.Equal(t, uint64(quicvarint.Max+3), conf.InitialConnectionReceiveWindow)
	require.Equal(t, uint64(quicvarint.Max+4), conf.MaxConnectionReceiveWindow)
	require.Equal(t, int64(1<<60+1), conf.MaxIncomingStreams)
	require.Equal(t, int64(1<<60+2), conf.MaxIncomingUniStreams)
	require.Equal(t, uint16(protocol.MaxPacketBufferSize+1), conf.InitialPacketSize)
	require.Equal(t, uint64(quicvarint.Max), prepared.InitialStreamReceiveWindow)
	require.Equal(t, uint64(quicvarint.Max), prepared.MaxStreamReceiveWindow)
	require.Equal(t, uint64(quicvarint.Max), prepared.InitialConnectionReceiveWindow)
	require.Equal(t, uint64(quicvarint.Max), prepared.MaxConnectionReceiveWindow)
	require.Equal(t, int64(1<<60), prepared.MaxIncomingStreams)
	require.Equal(t, int64(1<<60), prepared.MaxIncomingUniStreams)
	require.Equal(t, uint16(protocol.MaxPacketBufferSize), prepared.InitialPacketSize)
	require.Equal(t, []Version{0x1234}, prepared.Versions)
	require.Zero(t, nestedCallbackCalls)

	defaults := prepareConfigForClient(nil)
	require.Equal(t, protocol.SupportedVersions, defaults.Versions)
	require.EqualValues(t, protocol.DefaultInitialMaxStreamData, defaults.InitialStreamReceiveWindow)
	require.EqualValues(t, protocol.DefaultInitialMaxData, defaults.InitialConnectionReceiveWindow)
}

func TestConfigInitialReceiveWindowsCanBeEncoded(t *testing.T) {
	prepared, err := prepareConfig(&Config{
		InitialStreamReceiveWindow:     quicvarint.Max + 1,
		InitialConnectionReceiveWindow: quicvarint.Max + 1,
	})
	require.NoError(t, err)

	params := &wire.TransportParameters{
		InitialMaxStreamDataBidiLocal:  protocol.ByteCount(prepared.InitialStreamReceiveWindow),
		InitialMaxStreamDataBidiRemote: protocol.ByteCount(prepared.InitialStreamReceiveWindow),
		InitialMaxStreamDataUni:        protocol.ByteCount(prepared.InitialStreamReceiveWindow),
		InitialMaxData:                 protocol.ByteCount(prepared.InitialConnectionReceiveWindow),
		MaxBidiStreamNum:               protocol.StreamNum(prepared.MaxIncomingStreams),
		MaxUniStreamNum:                protocol.StreamNum(prepared.MaxIncomingUniStreams),
		MaxAckDelay:                    protocol.MaxAckDelayInclGranularity,
		AckDelayExponent:               protocol.AckDelayExponent,
		MaxUDPPayloadSize:              protocol.MaxPacketBufferSize,
		ActiveConnectionIDLimit:        protocol.MaxActiveConnectionIDs,
	}
	require.NotPanics(t, func() { params.Marshal(protocol.PerspectiveClient) })
}
