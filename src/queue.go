package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

const (
	workerCount = 10
	queueBuffer = 1024
	maxSSEClients = 500
)

// IngestQueue decouples HTTP receipt from store/detect/broadcast.
// The ingest handler calls Enqueue and returns immediately.
// Worker goroutines drain Records() and do the actual processing.
type IngestQueue interface {
	Enqueue(record HealthRecord) error
	Records() <-chan HealthRecord
}

// ─── ChannelQueue ─────────────────────────────────────────────────────────────
// In-process fallback used when SQS_QUEUE_URL is not set.
// Identical semantics to SQSQueue — swap one for the other via env var only.

type ChannelQueue struct {
	ch chan HealthRecord
}

func NewChannelQueue() *ChannelQueue {
	return &ChannelQueue{ch: make(chan HealthRecord, queueBuffer)}
}

func (q *ChannelQueue) Enqueue(record HealthRecord) error {
	select {
	case q.ch <- record:
		return nil
	default:
		return fmt.Errorf("channel queue full (buffer=%d)", queueBuffer)
	}
}

func (q *ChannelQueue) Records() <-chan HealthRecord {
	return q.ch
}

// ─── SQSQueue ─────────────────────────────────────────────────────────────────
// Publishes records to AWS SQS and polls for them via long-polling.
// Set SQS_QUEUE_URL to enable. AWS credentials come from the ECS task role.

type SQSQueue struct {
	client   *sqs.Client
	queueURL string
	out      chan HealthRecord
}

func NewSQSQueue(ctx context.Context, queueURL string) (*SQSQueue, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	q := &SQSQueue{
		client:   sqs.NewFromConfig(cfg),
		queueURL: queueURL,
		out:      make(chan HealthRecord, queueBuffer),
	}
	go q.poll(ctx)
	return q, nil
}

// Enqueue serialises the record to JSON and sends it to SQS.
// Returns immediately — the caller does not wait for processing.
func (q *SQSQueue) Enqueue(record HealthRecord) error {
	body, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = q.client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    aws.String(q.queueURL),
		MessageBody: aws.String(string(body)),
	})
	return err
}

func (q *SQSQueue) Records() <-chan HealthRecord {
	return q.out
}

// poll long-polls SQS and forwards received messages onto out.
// Deletes each message after forwarding — at-least-once delivery.
func (q *SQSQueue) poll(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		result, err := q.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(q.queueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     20, // long-poll reduces empty receives
		})
		if err != nil {
			log.Printf("[sqs] receive error: %v", err)
			continue
		}

		for _, msg := range result.Messages {
			var record HealthRecord
			if err := json.Unmarshal([]byte(aws.ToString(msg.Body)), &record); err != nil {
				log.Printf("[sqs] unmarshal error: %v", err)
			} else {
				q.out <- record
			}
			// Delete regardless — malformed messages must not block the queue
			q.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(q.queueURL),
				ReceiptHandle: msg.ReceiptHandle,
			})
		}
	}
}

// ─── Factory ──────────────────────────────────────────────────────────────────

// NewQueue returns SQSQueue if SQS_QUEUE_URL is set, ChannelQueue otherwise.
func NewQueue(ctx context.Context) IngestQueue {
	url := os.Getenv("SQS_QUEUE_URL")
	if url != "" {
		q, err := NewSQSQueue(ctx, url)
		if err != nil {
			log.Printf("[queue] SQS init failed (%v) — falling back to channel queue", err)
			return NewChannelQueue()
		}
		log.Printf("[queue] using SQS: %s", url)
		return q
	}
	log.Printf("[queue] SQS_QUEUE_URL not set — using in-process channel queue")
	return NewChannelQueue()
}

// ─── Worker pool ──────────────────────────────────────────────────────────────

// startWorkers launches workerCount goroutines that drain the queue.
// Each worker: Store.Add → AnomalyDetector.Detect → SSEBroker.Broadcast.
// This is the only place in the codebase that writes to the store or broadcasts.
func startWorkers(app *App) {
	for i := 0; i < workerCount; i++ {
		go func(id int) {
			for record := range app.Queue.Records() {
				app.Store.Add(record)
				app.Broker.Broadcast(SSEEvent{Event: "metric", Data: record})
				if anomaly := app.Detector.Detect(record); anomaly != nil {
					app.Store.AddAnomaly(*anomaly)
					app.Broker.Broadcast(SSEEvent{Event: "anomaly", Data: anomaly})
				}
			}
		}(i)
	}
	log.Printf("[queue] %d workers started", workerCount)
}
