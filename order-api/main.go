package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	amqp "github.com/rabbitmq/amqp091-go"
)

type OrderRequest struct {
	Amount     float64 `json:"amount"`
	Currency   string  `json:"currency"`
	CustomerID string  `json:"customerId"`
}

type OrderMessage struct {
	OrderID string       `json:"orderId"`
	Order   OrderRequest `json:"order"`
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	httpAddr := env("HTTP_ADDR", ":8080")
	natsURL := env("NATS_URL", "nats://localhost:4222")
	rmqURL := env("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	queue := env("RABBITMQ_QUEUE", "order.queue")

	// nats
	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Fatalf("NATS: %v", err)
	}
	defer nc.Close()

	// rabbitmq
	conn, err := amqp.Dial(rmqURL)
	if err != nil {
		log.Fatalf("RMQ dial: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("RMQ channel: %v", err)
	}
	defer ch.Close()

	_, err = ch.QueueDeclare(queue, true, false, false, false, nil)
	if err != nil {
		log.Fatalf("QueueDeclare: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/orders", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req OrderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		orderID := time.Now().UTC().Format("20060102T150405.000000000Z07:00")

		evt := map[string]any{
			"type":      "order.created",
			"orderId":   orderID,
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		}
		evtBytes, _ := json.Marshal(evt)
		if err := nc.Publish("order.created", evtBytes); err != nil {
			http.Error(w, "nats publish failed", http.StatusInternalServerError)
			return
		}

		msg := OrderMessage{OrderID: orderID, Order: req}
		body, _ := json.Marshal(msg)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ch.PublishWithContext(ctx, "", queue, false, false, amqp.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now(),
		}); err != nil {
			http.Error(w, "rabbitmq publish failed", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
	})

	log.Printf("order-api on %s", httpAddr)
	log.Fatal(http.ListenAndServe(httpAddr, mux))
}
