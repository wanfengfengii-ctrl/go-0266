// Command potatoeye-cutseed-sprout-gate is the PotatoEye HTTP backend entry
// point. It opens the SQLite WAL store (persisting across restart), wires it to
// the HTTP API and serves the JSON route surface (component traceability: Go
// HTTP API -> cmd).
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"potatoeye-cutseed-sprout-gate/api"
	"potatoeye-cutseed-sprout-gate/store"
)

func main() {
	addr := os.Getenv("POTATOEYE_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	dbPath := os.Getenv("POTATOEYE_DB")
	if dbPath == "" {
		dbPath = "potatoeye.db"
	}

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("potatoeye: open store: %v", err)
	}
	defer st.Close()

	srv := &http.Server{Addr: addr, Handler: api.NewServer(st).Handler()}

	go func() {
		log.Printf("potatoeye listening on %s (db %s)", addr, dbPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("potatoeye: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("potatoeye: shutdown: %v", err)
	}
}
