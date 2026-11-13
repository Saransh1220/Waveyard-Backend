-- RW-1-9 Down migration

UPDATE roles
SET permissions = array_remove(permissions, 'admin:orders:refund'),
    updated_at = NOW()
WHERE name = 'super_admin';

DROP TABLE IF EXISTS order_refunds CASCADE;
DROP TABLE payment_webhook_events;
ALTER TABLE payment_webhook_events_legacy RENAME TO payment_webhook_events;
DROP TABLE IF EXISTS order_idempotency_keys CASCADE;
ALTER TABLE orders DROP COLUMN IF EXISTS checkout_url;
