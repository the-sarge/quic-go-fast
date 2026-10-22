package qlogwriter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/qlogwriter/jsontext"

	"github.com/stretchr/testify/require"
)

type recordedWriterEvent struct {
	Time float64 `json:"time"`
	Name string  `json:"name"`
	Data struct {
		Message string `json:"message"`
	} `json:"data"`
}

func decodeWriterEvents(t *testing.T, data []byte) []recordedWriterEvent {
	t.Helper()
	records := bytes.Split(data, recordSeparator)
	require.GreaterOrEqual(t, len(records), 2) // empty prefix, then trace header
	var events []recordedWriterEvent
	for _, record := range records[2:] {
		var event recordedWriterEvent
		require.NoError(t, json.Unmarshal(record, &event))
		events = append(events, event)
	}
	return events
}

type timestampWriterEvent struct {
	testEvent
	encodedAt time.Time
}

func (e *timestampWriterEvent) Encode(enc *jsontext.Encoder, eventTime time.Time) error {
	e.encodedAt = eventTime
	return e.testEvent.Encode(enc, eventTime)
}

func TestFileSeqConcurrentTimestamps(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var sink bytes.Buffer
		start := time.Now()
		trace := NewFileSeq(nopWriteCloser(&sink))
		first, second := trace.AddProducer(), trace.AddProducer()
		go trace.Run()

		before := &timestampWriterEvent{testEvent: testEvent{message: "before"}}
		first.RecordEvent(before)
		time.Sleep(time.Millisecond)
		concurrent := []*timestampWriterEvent{
			{testEvent: testEvent{message: "first"}},
			{testEvent: testEvent{message: "second"}},
		}
		release := make(chan struct{})
		var recorded sync.WaitGroup
		for i, producer := range []Recorder{first, second} {
			recorded.Go(func() {
				<-release
				producer.RecordEvent(concurrent[i])
			})
		}
		close(release)
		recorded.Wait()
		time.Sleep(time.Millisecond)
		after := &timestampWriterEvent{testEvent: testEvent{message: "after"}}
		second.RecordEvent(after)
		require.NoError(t, first.Close())
		require.NoError(t, second.Close())

		events := decodeWriterEvents(t, sink.Bytes())
		require.Len(t, events, 4)
		require.Equal(t, "before", events[0].Data.Message)
		require.ElementsMatch(t, []string{"first", "second"}, []string{events[1].Data.Message, events[2].Data.Message})
		require.Equal(t, "after", events[3].Data.Message)
		for i, expected := range []float64{0, 1, 1, 2} {
			require.Equal(t, expected, events[i].Time)
			require.Equal(t, "transport:test_event", events[i].Name)
		}
		require.Equal(t, start, before.encodedAt)
		for _, event := range concurrent {
			require.Equal(t, start.Add(time.Millisecond), event.encodedAt)
		}
		require.Equal(t, start.Add(2*time.Millisecond), after.encodedAt)
	})
}

// The header is written before gates are installed. Only Run accesses Write,
// and Close starts after Run has finished; tests inspect the buffer at those boundaries.
type gatedQlogSink struct {
	bytes.Buffer
	writeGate    chan struct{}
	writeStarted chan struct{}
	writeErr     error
	closeGate    chan struct{}
	closeStarted chan struct{}
	closeCount   int
}

func (w *gatedQlogSink) Write(data []byte) (int, error) {
	if w.writeGate != nil {
		select {
		case w.writeStarted <- struct{}{}:
		default:
		}
		<-w.writeGate
	}
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return w.Buffer.Write(data)
}

func (w *gatedQlogSink) Close() error {
	w.closeCount++
	if w.closeStarted != nil {
		close(w.closeStarted)
	}
	if w.closeGate != nil {
		<-w.closeGate
	}
	return nil
}

type failingWriterEvent struct{}

func (failingWriterEvent) Name() string { return "transport:encoding_error" }

func (failingWriterEvent) Encode(*jsontext.Encoder, time.Time) error {
	return errors.New("event encoding failed")
}

func TestFileSeqBackpressureAndProducerClose(t *testing.T) {
	for _, failure := range []string{"none", "encoding", "write"} {
		t.Run(failure, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var logs bytes.Buffer
				previousOutput := log.Writer()
				log.SetOutput(&logs)
				defer log.SetOutput(previousOutput)

				sink := &gatedQlogSink{}
				trace := NewFileSeq(sink)
				producer, closingProducer := trace.AddProducer(), trace.AddProducer()
				sink.writeGate = make(chan struct{})
				sink.writeStarted = make(chan struct{}, 1)
				if failure == "write" {
					sink.writeErr = errors.New("sink write failed")
				}
				go trace.Run()
				var first Event = testEvent{message: "first"}
				if failure == "encoding" {
					first = failingWriterEvent{}
				}
				producer.RecordEvent(first)
				<-sink.writeStarted
				for i := range eventChanSize {
					producer.RecordEvent(testEvent{message: fmt.Sprintf("queued %d", i)})
				}
				recorded := make(chan struct{})
				go func() {
					producer.RecordEvent(testEvent{message: "last"})
					close(recorded)
				}()
				synctest.Wait() // the sink and the full-queue recorder are blocked
				select {
				case <-recorded:
					t.Fatal("recording completed while the queue was full")
				default:
				}
				closeStarted, closed := make(chan struct{}), make(chan struct{})
				go func() {
					close(closeStarted)
					_ = closingProducer.Close()
					close(closed)
				}()
				<-closeStarted
				close(sink.writeGate)
				<-recorded
				<-closed
				require.NoError(t, producer.Close())
				require.Equal(t, 1, sink.closeCount)
				require.Nil(t, trace.AddProducer())
				if failure != "none" {
					expected := "event encoding failed"
					if failure == "write" {
						expected = "sink write failed"
					}
					require.Contains(t, logs.String(), expected)
					return
				}
				events := decodeWriterEvents(t, sink.Bytes())
				require.Len(t, events, eventChanSize+2)
				require.Equal(t, "first", events[0].Data.Message)
				for i := range eventChanSize {
					require.Equal(t, fmt.Sprintf("queued %d", i), events[i+1].Data.Message)
				}
				require.Equal(t, "last", events[len(events)-1].Data.Message)
				for i := 1; i < len(events); i++ {
					require.LessOrEqual(t, events[i-1].Time, events[i].Time)
				}
			})
		})
	}
}

func TestFileSeqFinalDrainRejectsProducers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sink := &gatedQlogSink{}
		trace := NewFileSeq(sink)
		producer := trace.AddProducer()
		sink.writeGate = make(chan struct{})
		sink.writeStarted = make(chan struct{}, 1)
		sink.closeGate = make(chan struct{})
		sink.closeStarted = make(chan struct{})
		go trace.Run()
		for i := range 3 {
			producer.RecordEvent(testEvent{message: fmt.Sprintf("event %d", i)})
		}
		<-sink.writeStarted
		closed := make(chan struct{})
		go func() {
			_ = producer.Close()
			close(closed)
		}()
		synctest.Wait() // final Close is waiting for the gated consumer to drain
		require.Nil(t, trace.AddProducer())
		select {
		case <-sink.closeStarted:
			t.Fatal("sink closed before queued events were drained")
		default:
		}
		close(sink.writeGate)
		<-sink.closeStarted
		require.Nil(t, trace.AddProducer())
		events := decodeWriterEvents(t, sink.Bytes())
		require.Len(t, events, 3)
		for i, event := range events {
			require.Equal(t, fmt.Sprintf("event %d", i), event.Data.Message)
		}
		close(sink.closeGate)
		<-closed
		require.Equal(t, 1, sink.closeCount)
		require.Nil(t, trace.AddProducer())
	})
}
