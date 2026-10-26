package subscription

import (
	"testing"
	"time"

	"comment_service/internal/domain"
)

func TestPublishDeliversToPostSubscribers(t *testing.T) {
	sm := NewSubsctiptionManager()
	ch1 := sm.Subscribe("p1")
	other := sm.Subscribe("p2")

	sm.Publish("p1", domain.Comment{ID: "c1", PostID: "p1"})

	select {
	case c := <-ch1:
		if c.ID != "c1" {
			t.Fatalf("got %q, want c1", c.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber of p1 got nothing")
	}

	select {
	case c := <-other:
		t.Fatalf("subscriber of p2 got comment %q", c.ID)
	default:
	}
}

func TestPublishDoesNotBlockOnSlowSubscriber(t *testing.T) {
	sm := NewSubsctiptionManager()
	_ = sm.Subscribe("p1") // никто не читает

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			sm.Publish("p1", domain.Comment{ID: "c"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a subscriber that does not read")
	}
}

func TestUnsubscribe(t *testing.T) {
	sm := NewSubsctiptionManager()
	ch := sm.Subscribe("p1")
	sm.Unsubscribe("p1", ch)

	sm.Publish("p1", domain.Comment{ID: "c1"})

	select {
	case <-ch:
		t.Fatal("unsubscribed channel received a comment")
	default:
	}
}
