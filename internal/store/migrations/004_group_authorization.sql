ALTER TABLE bot_groups ADD COLUMN authorization VARCHAR(16) NOT NULL DEFAULT 'pending', ADD COLUMN authorization_reason VARCHAR(500) NOT NULL DEFAULT '';
UPDATE verification_sessions SET status='releasing',next_attempt_at=UTC_TIMESTAMP(6) WHERE status IN ('pending','completing','expiring');
UPDATE punishments SET status='skipped' WHERE status='pending';
