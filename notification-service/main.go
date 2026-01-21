package main

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nats-io/nats.go"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	natsURL := env("NATS_URL", "nats://localhost:4222")
	subject := env("NATS_SUBJECT", "order.*") // TP: wildcard
	queueGroup := env("NATS_QUEUE_GROUP", "notification-workers")

	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Fatalf("NATS connect: %v", err)
	}
	defer nc.Close()

	_, err = nc.QueueSubscribe(subject, queueGroup, func(m *nats.Msg) {
		var payload map[string]any
		if err := json.Unmarshal(m.Data, &payload); err != nil {
			log.Printf("Notification received on %s: %s", m.Subject, string(m.Data))
			return
		}
		log.Printf("Notification received on %s: %v", m.Subject, payload)
	})
	if err != nil {
		log.Fatalf("subscribe: %v", err)
	}

	if err := nc.Flush(); err != nil {
		log.Fatalf("flush: %v", err)
	}

	log.Printf("notification-service listening on %s (%s)", natsURL, subject)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("shutting down...")
}
