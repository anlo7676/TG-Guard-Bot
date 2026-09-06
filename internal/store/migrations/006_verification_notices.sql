ALTER TABLE verification_sessions ADD COLUMN notice_done BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE verification_sessions SET notice_done=TRUE WHERE status IN ('verified','expired','cancelled','blocked','left');
