# TP Go — NATS + RabbitMQ + Elasticsearch (Étapes 1 à 3)

## Prérequis (services up)
- NATS : `nats://localhost:4222` (monitoring : `http://localhost:8222`)
- RabbitMQ : `amqp://guest:guest@localhost:5672/` (UI/API : `http://localhost:15672`)
- Elasticsearch : `http://localhost:9200`

### Quick check
```bash
curl -i http://localhost:8222/varz
curl -u guest:guest -i http://localhost:15672/api/overview
curl -i http://localhost:9200
```

---

## 1) Run les 3 projets

> Dans **3 terminaux** séparés.

### Terminal A — order-api
```bash
cd eventing-lab/order-api
go run .
```

### Terminal B — notification-service (NATS `order.*`)
```bash
cd eventing-lab/notification-service
go run .
```

### Terminal C — order-processor (RabbitMQ -> Elasticsearch)
```bash
cd eventing-lab/order-processor
go run .
```

---

## 2) Tests cURL (end-to-end)

### 2.1 Créer une commande (order-api) — attendu: **201** et body vide
```bash
curl -i -X POST "http://localhost:8080/orders" \
  -H "Content-Type: application/json" \
  -d '{"amount":120.5,"currency":"EUR","customerId":"12345"}'
```

### 2.2 Rafale de commandes (10)
```bash
for i in $(seq 1 10); do
  curl -s -o /dev/null -w "%{http_code}\n" -X POST "http://localhost:8080/orders" \
    -H "Content-Type: application/json" \
    -d "{\"amount\":$i,\"currency\":\"EUR\",\"customerId\":\"c$i\"}"
done
```

> Validation NATS: regarde les logs du `notification-service` (tu dois voir les events `order.created`).

---

## 3) Vérifs RabbitMQ (order.queue)

### 3.1 Voir l’état de `order.queue` (messages ready/unacked)
```bash
curl -u guest:guest -s \
  "http://localhost:15672/api/queues/%2F/order.queue?columns=name,messages,messages_ready,messages_unacknowledged"
```

### 3.2 Peek 5 messages (sans les consommer définitivement)
```bash
curl -u guest:guest -s "http://localhost:15672/api/queues/%2F/order.queue/get" \
  -H "Content-Type: application/json" \
  -d '{"count":5,"ackmode":"ack_requeue_true","encoding":"auto","truncate":50000}'
```

> Attendu: payload de forme `{ "orderId": "...", "order": { ... } }`.

---

## 4) Vérifs Elasticsearch (index `orders`)

### 4.1 Index présent
```bash
curl -s "http://localhost:9200/_cat/indices/orders?v"
```

### 4.2 Derniers docs
```bash
curl -s "http://localhost:9200/orders/_search?pretty" \
  -H "Content-Type: application/json" \
  -d '{"sort":[{"processedAt":"desc"}],"size":10}'
```

### 4.3 Count
```bash
curl -s "http://localhost:9200/orders/_count?pretty"
```

---

## 5) (Option) Reset pour rejouer proprement

### Purger la queue RabbitMQ
```bash
curl -u guest:guest -X DELETE \
  "http://localhost:15672/api/queues/%2F/order.queue/contents"
```

### Supprimer l’index Elasticsearch
```bash
curl -X DELETE "http://localhost:9200/orders"
```