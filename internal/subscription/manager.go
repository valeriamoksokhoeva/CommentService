package subscription

import (
	"comment_service/internal/domain"
	"sync"
)

type SubscriptionManager struct {
	mu          sync.RWMutex
	subscribers map[string][]chan domain.Comment
}

func NewSubsctiptionManager() *SubscriptionManager {
	return &SubscriptionManager{subscribers: make(map[string][]chan domain.Comment)}
}

func (sm *SubscriptionManager) Subscribe(postID string) chan domain.Comment {
	ch := make(chan domain.Comment, 16)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	_, ok := sm.subscribers[postID]

	if ok {
		sm.subscribers[postID] = append(sm.subscribers[postID], ch)
	} else {
		sm.subscribers[postID] = []chan domain.Comment{ch}
	}

	return ch
}

func (sm *SubscriptionManager) Unsubscribe(postID string, ch chan domain.Comment) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	chans := sm.subscribers[postID]
	for i, c := range chans {
		if c == ch {
			chans = append(chans[:i], chans[i+1:]...)
			break
		}
	}
	if len(chans) == 0 {
		delete(sm.subscribers, postID)
	} else {
		sm.subscribers[postID] = chans
	}
}

func (sm *SubscriptionManager) Publish(postID string, comment domain.Comment) {
	chans := sm.getSubscribers(postID)
	for _, ch := range chans {
		select {
		case ch <- comment:
		default: //!!! если клиент не читает свой канал, то скип этого канала, и больше он своего сообщения не получит никогда!!!
		}
	}
}

func (sm *SubscriptionManager) getSubscribers(postID string) []chan domain.Comment {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	chans := sm.subscribers[postID]
	copyChans := make([]chan domain.Comment, len(chans))
	copy(copyChans, chans)
	return copyChans
}
