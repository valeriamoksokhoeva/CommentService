package main

import (
	"comment_service/config"
	"comment_service/graph"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"comment_service/internal/store"
	"comment_service/internal/subscription"
	"context"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/vektah/gqlparser/v2/ast"
	"strconv"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	var dataStore store.Repository

	switch cfg.Storage {
	case "memory":
		dataStore = store.NewMemoryStore()

	case "postgres":
		postgresStore, err := store.NewPostgresStore(
			context.Background(),
			cfg.DatabaseURL,
		)
		if err != nil {
			log.Fatalf("failed to connect to PostgreSQL: %v", err)
		}
		defer postgresStore.Close()

		dataStore = postgresStore
	}

	manager := subscription.NewSubsctiptionManager()
	resolver := &graph.Resolver{
		Store:             dataStore,
		SubscriberManager: manager,
	}

	srv := handler.New(
		graph.NewExecutableSchema(graph.Config{Resolvers: resolver}),
	)

	srv.AddTransport(transport.Websocket{
		KeepAlivePingInterval: 10 * time.Second,
	})

	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})

	srv.SetQueryCache(lru.New[*ast.QueryDocument](1000))

	srv.Use(extension.Introspection{})
	srv.Use(extension.AutomaticPersistedQuery{
		Cache: lru.New[string](100),
	})

	http.Handle("/", playground.Handler("GraphQL playground", "/query"))
	http.Handle("/query", srv)

	httpServer := &http.Server{
		Addr: ":" + strconv.Itoa(cfg.Port),
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	go func() {
		log.Printf(
			"connect to http://localhost:%d/ for GraphQL playground",
			cfg.Port,
		)

		if err := httpServer.ListenAndServe(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down very gracefully...")

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
