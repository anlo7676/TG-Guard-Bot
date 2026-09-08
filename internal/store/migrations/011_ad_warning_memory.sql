CREATE TABLE IF NOT EXISTS ad_warning_memory (
 chat_id BIGINT NOT NULL,
 user_id BIGINT NOT NULL,
 fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 message_id BIGINT NOT NULL,
 event_key VARCHAR(100) NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 PRIMARY KEY(chat_id,user_id,fingerprint)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
