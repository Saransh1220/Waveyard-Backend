package application

import (
	"context"
	"log"
	"math/rand"
	"time"
)

type WebhookWorkerConfig struct {
	WorkerID      string
	PollInterval  time.Duration
	BatchSize     int
	LeaseDuration time.Duration
}

// StartWebhookWorker continuously polls PostgreSQL for queued webhook events
// using FOR UPDATE SKIP LOCKED.
func StartWebhookWorker(ctx context.Context, svc PaymentService, cfg WebhookWorkerConfig) {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 3 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 10
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = 60 * time.Second
	}
	if cfg.WorkerID == "" {
		cfg.WorkerID = "payment-webhook-worker"
	}

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			processed, err := svc.ProcessNextWebhookBatch(ctx, cfg.WorkerID, cfg.BatchSize, cfg.LeaseDuration)
			if err != nil {
				log.Printf("[WebhookWorker] Error processing batch: %v", err)
			}
			if processed > 0 {
				time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)
			}
		}
	}
}
