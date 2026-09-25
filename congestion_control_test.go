package quic

import (
	"testing"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/testutils/events"
	"github.com/stretchr/testify/require"
)

func TestCongestionControlV1ValidationAndCopy(t *testing.T) {
	var absent *Config
	require.Equal(t, "reno", absent.CongestionControlV1())
	require.Error(t, absent.SetCongestionControlV1("bbrv3"))
	c := &Config{}
	require.Equal(t, "reno", c.CongestionControlV1())
	for _, name := range []string{"bbrv3", "reno"} {
		require.NoError(t, c.SetCongestionControlV1(name))
		for _, invalid := range []string{"", "BBRv3", "cubic"} {
			require.Error(t, c.SetCongestionControlV1(invalid))
			require.Equal(t, name, c.CongestionControlV1())
		}
		prepared, err := prepareConfig(c)
		require.NoError(t, err)
		for _, copy := range []*Config{c.Clone(), populateConfig(c), prepared, prepareConfigForClient(c)} {
			require.Equal(t, name, copy.CongestionControlV1())
			require.NoError(t, copy.SetCongestionControlV1(map[string]string{"reno": "bbrv3", "bbrv3": "reno"}[name]))
			require.Equal(t, name, c.CongestionControlV1())
		}
	}
}

func TestCongestionControlV1IndependentConnections(t *testing.T) {
	c := &Config{}
	require.NoError(t, c.SetCongestionControlV1("bbrv3"))
	client := newClientTestConnection(t, nil, c, false).conn
	server := newServerTestConnection(t, nil, c, false).conn
	require.Equal(t, "bbrv3", client.CongestionControlV1())
	require.Equal(t, "bbrv3", server.CongestionControlV1())
	require.NotNil(t, client.emission.bbr)
	require.NotNil(t, server.emission.bbr)
	require.NotSame(t, client.emission.bbr.controller, server.emission.bbr.controller)
	require.NotSame(t, client.emission.bbr.credit, server.emission.bbr.credit)
	require.NoError(t, c.SetCongestionControlV1("reno"))
	require.Equal(t, "bbrv3", client.CongestionControlV1())
	require.Equal(t, "bbrv3", server.CongestionControlV1())
}

func TestCongestionControlV1DefaultReno(t *testing.T) {
	for _, c := range []*Config{nil, {}, {congestionControl: "reno"}} {
		for _, conn := range []*Conn{newClientTestConnection(t, nil, c, false).conn, newServerTestConnection(t, nil, c, false).conn} {
			require.Equal(t, "reno", conn.CongestionControlV1())
			require.Nil(t, conn.emission.bbr)
		}
	}
}

func TestCongestionControlV1Diagnostics(t *testing.T) {
	conf := &Config{InitialPacketSize: 1200, EnableDatagrams: true}
	require.NoError(t, conf.SetCongestionControlV1("bbrv3"))
	c := newConfiguredEmissionTestConnection(t, false, conf).conn
	recorder := &events.Recorder{}
	c.qlogger = recorder
	now := monotime.Now()
	require.NoError(t, c.triggerSending(now).err)
	require.NoError(t, c.triggerSending(now).err)
	records := recorder.Events(qlog.DebugEvent{})
	require.Len(t, records, 1, "diagnostic snapshots are rate bounded")
	diagnostic := records[0].(qlog.DebugEvent)
	require.Equal(t, "congestion_control", diagnostic.EventName)
	for _, field := range []string{"selected=bbrv3", "policy=bbrv3-draft06-classic-ecn-v1", "phase=Startup", "pacing=", "window=", "quantum=", "ce_active=", "recovery=", "ecn=", "limitation=", "pending=", "sample_valid=", "retained="} {
		require.Contains(t, diagnostic.Message, field)
	}
}
