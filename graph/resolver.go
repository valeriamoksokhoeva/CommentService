package graph

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require
// here.

import (
	"comment_service/internal/store"
	"comment_service/internal/subscription"
)

type Resolver struct {
	Store             store.Repository
	SubscriberManager *subscription.SubscriptionManager
}
