-- RW-1-9 Down migration

UPDATE roles
SET permissions = array_remove(permissions, 'admin:orders:refund'),
    updated_at = NOW()
WHERE name = 'super_admin';

DROP TABLE IF EXISTS order_refunds CASCADE;
DROP TABLE IF EXISTS payment_webhook_events CASCADE;
DROP TABLE IF EXISTS order_idempotency_keys CASCADE;
ALTER TABLE orders DROP COLUMN IF EXISTS checkout_url;
