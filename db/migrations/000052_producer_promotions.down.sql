UPDATE roles
SET permissions = array_remove(permissions, 'promotions:manage'), updated_at = NOW()
WHERE name = 'producer' AND 'promotions:manage' = ANY(permissions);

DROP TABLE producer_promotions, producer_promotion_revisions;
