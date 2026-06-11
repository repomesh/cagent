package runtime

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestElicitationError_Error(t *testing.T) {
	t.Parallel()

	err := &ElicitationError{Action: "decline", Message: "user said no"}
	assert.Equal(t, "elicitation decline: user said no", err.Error())
}

func TestElicitationBridge_SendBeforeSwapReturnsError(t *testing.T) {
	t.Parallel()

	var b elicitationBridge
	err := b.send(Error("nothing"))
	assert.ErrorIs(t, err, errNoElicitationChannel)
}

func TestElicitationBridge_SwapReturnsPrevious(t *testing.T) {
	t.Parallel()

	var b elicitationBridge
	first := make(chan Event, 1)
	second := make(chan Event, 1)

	prev := b.swap(first)
	assert.Nil(t, prev, "first swap should return nil prev")

	prev = b.swap(second)
	assert.Equal(t, first, prev, "swap should return the previously stored channel")

	prev = b.swap(nil)
	assert.Equal(t, second, prev, "swap(nil) should return the previously stored channel")
}

func TestElicitationBridge_SendDeliversToCurrentChannel(t *testing.T) {
	t.Parallel()

	var b elicitationBridge
	ch := make(chan Event, 1)
	b.swap(ch)

	require.NoError(t, b.send(Error("hello")))

	select {
	case ev := <-ch:
		ee, ok := ev.(*ErrorEvent)
		require.True(t, ok)
		assert.Equal(t, "hello", ee.Error)
	case <-time.After(time.Second):
		t.Fatal("expected event, none received")
	}
}

func TestElicitationBridge_SendRecoversClosedChannel(t *testing.T) {
	t.Parallel()

	var b elicitationBridge
	ch := make(chan Event)
	b.swap(ch)
	close(ch)

	err := b.send(Error("closed"))
	assert.ErrorIs(t, err, errNoElicitationChannel)
}

// TestElicitationBridge_RestoreAndCloseWaitsForInflightSenders is the
// regression test for issue #3069: stream teardown must not close an event
// channel while an MCP elicitation goroutine is blocked sending to it.
//
// The test parks a send on the current channel, starts restoreAndClose, and
// verifies teardown cannot close the channel until the parked send drains.
// Running under -race exercises the close-vs-send coordination that used to
// panic with "send on closed channel".
func TestElicitationBridge_RestoreAndCloseWaitsForInflightSenders(t *testing.T) {
	t.Parallel()

	var b elicitationBridge
	current := make(chan Event)
	parent := make(chan Event, 1)
	b.swap(current)

	sendStarted := make(chan struct{})
	sendDone := make(chan error, 1)
	go func() {
		close(sendStarted)
		sendDone <- b.send(Error("inflight"))
	}()
	<-sendStarted

	// Give the sender a moment to grab the RLock and park on the channel.
	time.Sleep(20 * time.Millisecond)

	closed := make(chan struct{})
	go func() {
		b.restoreAndClose(current, parent, StreamStopped("session", "agent", turnEndReasonNormal))
		close(closed)
	}()

	select {
	case <-closed:
		t.Fatal("channel closed while a send was still in flight")
	case <-time.After(50 * time.Millisecond):
	}

	select {
	case ev := <-current:
		ee, ok := ev.(*ErrorEvent)
		require.True(t, ok)
		assert.Equal(t, "inflight", ee.Error)
	case <-time.After(time.Second):
		t.Fatal("expected in-flight event")
	}

	select {
	case err := <-sendDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("in-flight send never completed after reader drained")
	}

	select {
	case ev := <-current:
		require.IsType(t, &StreamStoppedEvent{}, ev)
	case <-time.After(time.Second):
		t.Fatal("expected final stream stopped event")
	}

	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("restoreAndClose never completed after final event drained")
	}

	select {
	case _, ok := <-current:
		assert.False(t, ok, "current channel should be closed after StreamStopped")
	default:
		t.Fatal("current channel should be closed")
	}
}

// TestElicitationBridge_ConcurrentSendsAndCloseAreSerializedSafely runs many
// concurrent sends while closing the stream under -race to confirm the bridge
// owns all close-vs-send synchronization.
func TestElicitationBridge_ConcurrentSendsAndCloseAreSerializedSafely(t *testing.T) {
	t.Parallel()

	var b elicitationBridge
	ch := make(chan Event, 64)
	parent := make(chan Event, 1)
	b.swap(ch)

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			for range 5 {
				_ = b.send(Error("x"))
			}
		})
	}

	received := make(chan struct{})
	go func() {
		defer close(received)
		for range ch {
		}
	}()

	wg.Wait()
	b.restoreAndClose(ch, parent, StreamStopped("session", "agent", turnEndReasonNormal))

	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("reader did not observe channel close")
	}
}
