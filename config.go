package quic

import (
	"fmt"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/quicvarint"
)

// Clone clones a Config.
func (c *Config) Clone() *Config {
	copy := *c
	return &copy
}

func (c *Config) handshakeTimeout() time.Duration {
	return 2 * c.HandshakeIdleTimeout
}

func (c *Config) maxRetryTokenAge() time.Duration {
	return c.handshakeTimeout()
}

func prepareConfig(config *Config) (*Config, error) {
	prepared := clipConfig(config)
	if config == nil {
		return populateConfig(prepared), nil
	}
	// Preserve the caller-visible clipping performed by the old validation path.
	// Defaults, negative stream conversion, and the newly enforced initial window
	// bounds only apply to the effective copy.
	if config.MaxIncomingStreams != prepared.MaxIncomingStreams {
		config.MaxIncomingStreams = prepared.MaxIncomingStreams
	}
	if config.MaxIncomingUniStreams != prepared.MaxIncomingUniStreams {
		config.MaxIncomingUniStreams = prepared.MaxIncomingUniStreams
	}
	if config.MaxStreamReceiveWindow != prepared.MaxStreamReceiveWindow {
		config.MaxStreamReceiveWindow = prepared.MaxStreamReceiveWindow
	}
	if config.MaxConnectionReceiveWindow != prepared.MaxConnectionReceiveWindow {
		config.MaxConnectionReceiveWindow = prepared.MaxConnectionReceiveWindow
	}
	if config.InitialPacketSize != prepared.InitialPacketSize {
		config.InitialPacketSize = prepared.InitialPacketSize
	}

	// Check that all QUIC versions are actually supported.
	for _, v := range config.Versions {
		if !protocol.IsValidVersion(v) {
			return nil, fmt.Errorf("invalid QUIC version: %s", v)
		}
	}
	return populateConfig(prepared), nil
}

func prepareConfigForClient(config *Config) *Config {
	return populateConfig(clipConfig(config))
}

func clipConfig(config *Config) *Config {
	if config == nil {
		config = &Config{}
	}
	clipped := config.Clone()
	const maxStreams = 1 << 60
	if clipped.MaxIncomingStreams > maxStreams {
		clipped.MaxIncomingStreams = maxStreams
	}
	if clipped.MaxIncomingUniStreams > maxStreams {
		clipped.MaxIncomingUniStreams = maxStreams
	}
	if clipped.InitialStreamReceiveWindow > quicvarint.Max {
		clipped.InitialStreamReceiveWindow = quicvarint.Max
	}
	if clipped.MaxStreamReceiveWindow > quicvarint.Max {
		clipped.MaxStreamReceiveWindow = quicvarint.Max
	}
	if clipped.InitialConnectionReceiveWindow > quicvarint.Max {
		clipped.InitialConnectionReceiveWindow = quicvarint.Max
	}
	if clipped.MaxConnectionReceiveWindow > quicvarint.Max {
		clipped.MaxConnectionReceiveWindow = quicvarint.Max
	}
	if clipped.InitialPacketSize > 0 && clipped.InitialPacketSize < protocol.MinInitialPacketSize {
		clipped.InitialPacketSize = protocol.MinInitialPacketSize
	}
	if clipped.InitialPacketSize > protocol.MaxPacketBufferSize {
		clipped.InitialPacketSize = protocol.MaxPacketBufferSize
	}
	return clipped
}

// populateConfig populates fields in the quic.Config with their default values, if none are set
// it may be called with nil
func populateConfig(config *Config) *Config {
	if config == nil {
		config = &Config{}
	}
	versions := config.Versions
	if len(versions) == 0 {
		versions = protocol.SupportedVersions
	}
	handshakeIdleTimeout := protocol.DefaultHandshakeIdleTimeout
	if config.HandshakeIdleTimeout != 0 {
		handshakeIdleTimeout = config.HandshakeIdleTimeout
	}
	idleTimeout := protocol.DefaultIdleTimeout
	if config.MaxIdleTimeout != 0 {
		idleTimeout = config.MaxIdleTimeout
	}
	initialStreamReceiveWindow := config.InitialStreamReceiveWindow
	if initialStreamReceiveWindow == 0 {
		initialStreamReceiveWindow = protocol.DefaultInitialMaxStreamData
	}
	maxStreamReceiveWindow := config.MaxStreamReceiveWindow
	if maxStreamReceiveWindow == 0 {
		maxStreamReceiveWindow = protocol.DefaultMaxReceiveStreamFlowControlWindow
	}
	initialConnectionReceiveWindow := config.InitialConnectionReceiveWindow
	if initialConnectionReceiveWindow == 0 {
		initialConnectionReceiveWindow = protocol.DefaultInitialMaxData
	}
	maxConnectionReceiveWindow := config.MaxConnectionReceiveWindow
	if maxConnectionReceiveWindow == 0 {
		maxConnectionReceiveWindow = protocol.DefaultMaxReceiveConnectionFlowControlWindow
	}
	maxIncomingStreams := config.MaxIncomingStreams
	if maxIncomingStreams == 0 {
		maxIncomingStreams = protocol.DefaultMaxIncomingStreams
	} else if maxIncomingStreams < 0 {
		maxIncomingStreams = 0
	}
	maxIncomingUniStreams := config.MaxIncomingUniStreams
	if maxIncomingUniStreams == 0 {
		maxIncomingUniStreams = protocol.DefaultMaxIncomingUniStreams
	} else if maxIncomingUniStreams < 0 {
		maxIncomingUniStreams = 0
	}
	initialPacketSize := config.InitialPacketSize
	if initialPacketSize == 0 {
		initialPacketSize = protocol.InitialPacketSize
	}

	return &Config{
		GetConfigForClient:               config.GetConfigForClient,
		Versions:                         versions,
		HandshakeIdleTimeout:             handshakeIdleTimeout,
		MaxIdleTimeout:                   idleTimeout,
		KeepAlivePeriod:                  config.KeepAlivePeriod,
		InitialStreamReceiveWindow:       initialStreamReceiveWindow,
		MaxStreamReceiveWindow:           maxStreamReceiveWindow,
		InitialConnectionReceiveWindow:   initialConnectionReceiveWindow,
		MaxConnectionReceiveWindow:       maxConnectionReceiveWindow,
		AllowConnectionWindowIncrease:    config.AllowConnectionWindowIncrease,
		MaxIncomingStreams:               maxIncomingStreams,
		MaxIncomingUniStreams:            maxIncomingUniStreams,
		TokenStore:                       config.TokenStore,
		EnableDatagrams:                  config.EnableDatagrams,
		InitialPacketSize:                initialPacketSize,
		DisablePathMTUDiscovery:          config.DisablePathMTUDiscovery,
		EnableStreamResetPartialDelivery: config.EnableStreamResetPartialDelivery,
		Allow0RTT:                        config.Allow0RTT,
		Tracer:                           config.Tracer,
	}
}
