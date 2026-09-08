CREATE TABLE IF NOT EXISTS authorization_epochs (
 chat_id BIGINT PRIMARY KEY, version BIGINT NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS punishment_workflows (
 event_key VARCHAR(100) PRIMARY KEY, authority_version BIGINT NOT NULL DEFAULT 0,
 started BOOLEAN NOT NULL DEFAULT FALSE, kick_banned BOOLEAN NOT NULL DEFAULT FALSE,
 release_needed BOOLEAN NOT NULL DEFAULT FALSE, release_at DATETIME(6) NULL,
 attempts INT NOT NULL DEFAULT 0, next_attempt_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 INDEX idx_workflow_recovery(next_attempt_at), INDEX idx_workflow_release(release_needed,release_at)
);
INSERT IGNORE INTO punishment_workflows(event_key,authority_version)
 SELECT p.event_key,COALESCE(a.version,0) FROM punishments p LEFT JOIN authorization_epochs a ON a.chat_id=p.chat_id WHERE p.status='pending';
