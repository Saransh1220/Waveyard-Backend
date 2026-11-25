CREATE TABLE producer_promotions (
    id uuid PRIMARY KEY,
    producer_id uuid NOT NULL REFERENCES users(id),
    revision bigint NOT NULL CHECK (revision > 0),
    published_revision bigint CHECK (published_revision > 0 AND published_revision <= revision),
    updated_at timestamptz NOT NULL,
    UNIQUE (id, producer_id)
);

CREATE INDEX producer_promotions_owner_updated_idx
    ON producer_promotions (producer_id, updated_at DESC, id DESC);

CREATE TABLE producer_promotion_revisions (
    promotion_id uuid NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    actor_id uuid NOT NULL,
    action text NOT NULL CHECK (action IN ('create', 'edit', 'publish', 'pause', 'archive')),
    state text NOT NULL CHECK (state IN ('draft', 'published', 'paused', 'archived')),
    rules jsonb NOT NULL CHECK (jsonb_typeof(rules) = 'object'),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (promotion_id, revision),
    FOREIGN KEY (promotion_id, actor_id) REFERENCES producer_promotions (id, producer_id)
);

ALTER TABLE producer_promotions
    ADD FOREIGN KEY (id, revision) REFERENCES producer_promotion_revisions (promotion_id, revision)
    DEFERRABLE INITIALLY DEFERRED,
    ADD FOREIGN KEY (id, published_revision) REFERENCES producer_promotion_revisions (promotion_id, revision)
    DEFERRABLE INITIALLY DEFERRED;

UPDATE roles
SET permissions = array_append(permissions, 'promotions:manage'), updated_at = NOW()
WHERE name = 'producer' AND NOT ('promotions:manage' = ANY(permissions));
