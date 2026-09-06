CREATE TABLE IF NOT EXISTS welcome_cleanup (
 chat_id BIGINT NOT NULL,message_id BIGINT NOT NULL,delete_at DATETIME(6) NOT NULL,
 next_attempt_at DATETIME(6) NOT NULL,attempts INT NOT NULL DEFAULT 0,
 last_error VARCHAR(500) NOT NULL DEFAULT '',done BOOLEAN NOT NULL DEFAULT FALSE,
 PRIMARY KEY(chat_id,message_id),INDEX idx_welcome_due(done,next_attempt_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
