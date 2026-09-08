package store

import "context"

func (s *Store) RecordUpgrade(ctx context.Context, version string) error {
	_, err := s.DB.ExecContext(ctx, "INSERT INTO admin_audits(chat_id,actor_id,action,old_value,new_value) VALUES(0,0,'system.upgrade.request',JSON_OBJECT(),?)", SQLJSON(map[string]string{"version": version}))
	return err
}
