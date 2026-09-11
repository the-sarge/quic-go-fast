package http3

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/stretchr/testify/require"
)

func TestParsePriority(t *testing.T) {
	tests := []struct {
		name            string
		value           string
		wantUrgency     int8
		wantIncremental bool
	}{
		{name: "empty", wantUrgency: defaultPriorityUrgency},
		{name: "urgency", value: "u=0", wantUrgency: 0},
		{name: "implicit incremental", value: "i", wantUrgency: defaultPriorityUrgency, wantIncremental: true},
		{name: "explicit incremental", value: "i=?1", wantUrgency: defaultPriorityUrgency, wantIncremental: true},
		{name: "urgency and non-incremental", value: "i=?0, u=4", wantUrgency: 4},
		{name: "unknown fields", value: "some=data;someparam;u=fake, u=1;foo, i;bar", wantUrgency: 1, wantIncremental: true},
		{name: "repeated fields", value: "u=1,i,u=5,i=?0", wantUrgency: 5},
		{name: "wrong types", value: `u="ignored", i=1`, wantUrgency: defaultPriorityUrgency},
		{name: "out of range urgency", value: "u=8", wantUrgency: defaultPriorityUrgency},
		{name: "valid unknown SFV types", value: `a=(1 "two" ?1);x, b=:YWJj:, c=%"hello%20world", u=2`, wantUrgency: 2},
		{name: "semicolon inside an extension value", value: `a="x;y";q=1, u=2`, wantUrgency: 2},
		{name: "known fields inside extension values", value: `future="x\",u=0,i", u=5`, wantUrgency: 5},
		{name: "backslash inside a display string", value: `future=%"x\", u=5`, wantUrgency: 5},
		{name: "invalid member", value: "u=0, bad=", wantUrgency: defaultPriorityUrgency},
		{name: "unterminated string", value: `u=1,i, invalid="`, wantUrgency: defaultPriorityUrgency},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			urgency, incremental := parsePriority(test.value)
			require.Equal(t, test.wantUrgency, urgency)
			require.Equal(t, test.wantIncremental, incremental)
		})
	}
}

func FuzzParsePriority(f *testing.F) {
	for _, value := range []string{"", "u=0", "i=?0, u=7", `future="x,u=0", u=5`, `u=1, invalid="`} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		urgency, _ := parsePriority(value)
		if urgency < 0 || urgency > 7 {
			t.Fatalf("invalid urgency: %d", urgency)
		}
	})
}

func TestServerRequestPriority(t *testing.T) {
	type request struct {
		name            string
		priority        []string
		wantUrgency     int8
		wantIncremental bool
	}
	for _, scenario := range []struct {
		name     string
		requests []request
	}{
		{
			name: "priority-aware connection",
			requests: []request{
				{name: "before priority signal", wantUrgency: 3, wantIncremental: true},
				{name: "explicit urgency", priority: []string{"u=0"}, wantUrgency: 0},
				{name: "after priority signal", wantUrgency: 3},
				{name: "incremental", priority: []string{"i"}, wantUrgency: 3, wantIncremental: true},
				{name: "multiple field lines", priority: []string{"u=2", "i"}, wantUrgency: 2, wantIncremental: true},
			},
		},
		{
			name: "malformed priority signal",
			requests: []request{
				{name: "malformed header", priority: []string{`u=0, invalid="`}, wantUrgency: 3},
				{name: "after malformed signal", wantUrgency: 3},
			},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var recorder events.Recorder
			client, server := newConnPair(t, withServerRecorder(&recorder))
			conn := newRawServerConn(server, false, 0, nil, nil, context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), 0)
			for _, test := range scenario.requests {
				t.Run(test.name, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					str, err := client.OpenStreamSync(ctx)
					require.NoError(t, err)
					require.NoError(t, str.SetDeadline(time.Now().Add(time.Second)))
					req := httptest.NewRequest(http.MethodGet, "https://www.example.com", nil)
					for _, value := range test.priority {
						req.Header.Add("Priority", value)
					}
					_, err = str.Write(encodeRequest(t, req))
					require.NoError(t, err)
					require.NoError(t, str.Close())
					incoming, err := server.AcceptStream(ctx)
					require.NoError(t, err)
					require.NoError(t, incoming.SetDeadline(time.Now().Add(time.Second)))
					// Start at a different priority so even applying the default emits an event.
					incoming.SetPriority(7, false)
					recorder.Clear()

					conn.HandleRequestStream(incoming)

					require.Equal(t, []qlogwriter.Event{
						qlog.StreamPriorityUpdated{StreamID: incoming.StreamID(), Urgency: test.wantUrgency, Incremental: test.wantIncremental},
					}, recorder.Events(qlog.StreamPriorityUpdated{}))
					_, err = io.Copy(io.Discard, str)
					require.NoError(t, err)
				})
			}
		})
	}
}
