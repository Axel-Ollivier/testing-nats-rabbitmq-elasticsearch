package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func ensureIndex(es *elasticsearch.Client, index string) error {
	res, err := es.Indices.Exists([]string{index})
	if err != nil {
		return err
	}
	res.Body.Close()

	if res.StatusCode == 200 {
		return nil
	}
	if res.StatusCode != 404 {
		return fmt.Errorf("indices.exists returned %d", res.StatusCode)
	}

	createRes, err := es.Indices.Create(index)
	if err != nil {
		return err
	}
	defer createRes.Body.Close()

	if createRes.IsError() {
		return fmt.Errorf("indices.create error: %s", createRes.Status())
	}
	return nil
}

func indexProcessed(es *elasticsearch.Client, index, orderID string) error {
	doc := map[string]any{
		"orderId":     orderID,
		"status":      "PROCESSED",
		"processedAt": time.Now().UTC().Format(time.RFC3339),
		"source":      "rabbitmq",
	}
	body, _ := json.Marshal(doc)

	req := esapi.IndexRequest{
		Index: index,
		Body:  bytes.NewReader(body),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res, err := req.Do(ctx, es)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("index error: %s", res.Status())
	}
	return nil
}

func main() {
	rmqURL := env("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	queue := env("RABBITMQ_QUEUE", "order.queue")

	esURL := env("ES_URL", "http://localhost:9200")
	esIndex := env("ES_INDEX", "orders")
	esUser := os.Getenv("ES_USER")
	esPass := os.Getenv("ES_PASS")

	es, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: []string{esURL},
		Username:  esUser,
		Password:  esPass,
	})
	if err != nil {
		log.Fatalf("ES client: %v", err)
	}
	if err := ensureIndex(es, esIndex); err != nil {
		log.Fatalf("ES ensure index: %v", err)
	}

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

	_ = ch.Qos(1, 0, false)

	msgs, err := ch.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		log.Fatalf("Consume: %v", err)
	}

	log.Printf("order-processor consuming %s, indexing to ES %s/%s", queue, esURL, esIndex)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-stop:
			log.Println("shutting down...")
			return

		case d, ok := <-msgs:
			if !ok {
				log.Println("consumer closed")
				return
			}

			var message map[string]any
			if err := json.Unmarshal(d.Body, &message); err != nil {
				log.Printf("json invalid: %s", string(d.Body))
				_ = d.Nack(false, true)
				continue
			}

			log.Printf("Processing order: %v", message)

			time.Sleep(2 * time.Second)

			orderID, _ := message["orderId"].(string)
			if orderID == "" {
				_ = d.Nack(false, true)
				continue
			}

			if err := indexProcessed(es, esIndex, orderID); err != nil {
				log.Printf("ES index failed: %v", err)
				_ = d.Nack(false, true)
				continue
			}

			if err := d.Ack(false); err != nil {
				log.Printf("ACK failed: %v", err)
			}
		}
	}
}
