CREATE TABLE IF NOT EXISTS profile_avatar_samples (
 chat_id BIGINT NOT NULL,
 avatar_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 source_user_id BIGINT NOT NULL,
 matched_domain VARCHAR(253) NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 PRIMARY KEY(chat_id,avatar_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
