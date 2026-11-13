-- A verified event may precede its local order association.
ALTER TABLE payment_webhook_events DROP CONSTRAINT IF EXISTS payment_webhook_events_order_id_fkey;

-- Different keys must not allow concurrent full refunds of the same capture.
CREATE UNIQUE INDEX uq_order_refunds_active_payment ON order_refunds(payment_id)
    WHERE status <> 'failed';
