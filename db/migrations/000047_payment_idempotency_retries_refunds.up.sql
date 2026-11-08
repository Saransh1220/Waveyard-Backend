-- RW-1-9: Payment idempotency, webhook retries and refunds

-- 1. Orders: persist hosted checkout URL for idempotent recovery
ALTER TABLE orders ADD COLUMN IF NOT EXISTS checkout_url TEXT;

-- 2. Order idempotency keys: reserve intent before outbound network I/O
CREATE TABLE IF NOT EXISTS order_idempotency_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    idempotency_key VARCHAR(128) NOT NULL,
    request_hash VARCHAR(64) NOT NULL,
    order_id UUID REFERENCES orders(id) ON DELETE SET NULL,
    checkout_state VARCHAR(50) NOT NULL DEFAULT 'reserved' CHECK (
        checkout_state IN ('reserved', 'claiming', 'ready', 'failed', 'reconciling')
    ),
    provider VARCHAR(40) NOT NULL,
    provider_checkout_id VARCHAR(255),
    checkout_url TEXT,
    attempt_generation INTEGER NOT NULL DEFAULT 1,
    lease_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_order_idempotency_user_key UNIQUE (user_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_order_idempotency_order_id ON order_idempotency_keys(order_id);

-- 3. Webhook events inbox: durable receipt before ACK and async retry processing
CREATE TABLE IF NOT EXISTS payment_webhook_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider VARCHAR(40) NOT NULL,
    event_id VARCHAR(255) NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    order_id UUID REFERENCES orders(id) ON DELETE SET NULL,
    provider_checkout_id VARCHAR(255),
    provider_payment_id VARCHAR(255),
    reported_amount INTEGER,
    reported_currency VARCHAR(3),
    payload JSONB NOT NULL,
    headers JSONB,
    status VARCHAR(50) NOT NULL DEFAULT 'queued' CHECK (
        status IN ('queued', 'processing', 'completed', 'failed', 'needs_review', 'ignored')
    ),
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 5,
    last_error TEXT,
    worker_id VARCHAR(120),
    locked_at TIMESTAMPTZ,
    lease_expires_at TIMESTAMPTZ,
    next_retry_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_payment_webhook_events_provider_event UNIQUE (provider, event_id)
);

CREATE INDEX IF NOT EXISTS idx_payment_webhook_events_poll ON payment_webhook_events(status, next_retry_at) WHERE status IN ('queued', 'failed');
CREATE INDEX IF NOT EXISTS idx_payment_webhook_events_order_id ON payment_webhook_events(order_id);

-- 4. Order refunds: track admin full refund operations and provider confirmations
CREATE TABLE IF NOT EXISTS order_refunds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
    payment_id UUID NOT NULL REFERENCES payments(id) ON DELETE RESTRICT,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    admin_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    idempotency_key VARCHAR(128) NOT NULL,
    amount INTEGER NOT NULL CHECK (amount > 0),
    currency VARCHAR(3) NOT NULL,
    provider VARCHAR(40) NOT NULL,
    provider_refund_id VARCHAR(255),
    provider_payment_id VARCHAR(255) NOT NULL,
    reason VARCHAR(100) NOT NULL,
    notes TEXT,
    status VARCHAR(50) NOT NULL DEFAULT 'requested' CHECK (
        status IN ('requested', 'submitting', 'pending', 'succeeded', 'failed', 'needs_review')
    ),
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_order_refunds_idempotency UNIQUE (idempotency_key)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_order_refunds_provider_ref
    ON order_refunds(provider, provider_refund_id)
    WHERE provider_refund_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_order_refunds_order_id ON order_refunds(order_id);

-- 5. RBAC permission grant for admin refund operations
UPDATE roles
SET permissions = array_append(permissions, 'admin:orders:refund'),
    updated_at = NOW()
WHERE name = 'super_admin' AND NOT ('admin:orders:refund' = ANY(permissions));
