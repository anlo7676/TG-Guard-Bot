package store

import (
	"context"
	"fmt"
)

// View identifies a fixed, parameterized read model; callers never supply SQL.
type View int

const (
	ViewDashboard         View = 0
	ViewGroups            View = 1
	ViewLists             View = 2
	ViewLogs              View = 3
	ViewUsers             View = 4
	ViewPunishments       View = 5
	ViewVerifications     View = 6
	ViewAudits            View = 7
	ViewDeadUpdates       View = 8
	ViewUserVerifications View = 9
	ViewUserReviews       View = 10
	ViewUserPunishments   View = 11
	ViewGroupStats        View = 12
	ViewListSummary       View = 13
	ViewListMenu          View = 14
)

var viewSQL = map[View]string{
	ViewDashboard:         `SELECT (SELECT COUNT(*) FROM bot_groups WHERE active=TRUE) AS groups_count,(SELECT COUNT(*) FROM users) AS users_count,(SELECT COUNT(*) FROM moderation_logs WHERE created_at>=UTC_DATE()) AS today_reviews,(SELECT COUNT(*) FROM punishments WHERE created_at>=UTC_DATE() AND deleted=TRUE) AS today_deletes,(SELECT COUNT(*) FROM ai_usage_logs WHERE created_at>=UTC_DATE() AND cached=FALSE) AS today_ai_calls,(SELECT COALESCE(SUM(input_tokens+output_tokens),0) FROM ai_usage_logs WHERE created_at>=UTC_DATE()) AS today_ai_tokens,(SELECT COUNT(*) FROM update_inbox WHERE status='dead') AS dead_updates`,
	ViewGroups:            `SELECT chat_id,title,active,authorization,authorization_reason,created_at,updated_at FROM bot_groups WHERE chat_id<? ORDER BY chat_id DESC LIMIT 100`,
	ViewLists:             `SELECT id,chat_id,user_id,username,kind,reason,expires_at FROM list_entries WHERE chat_id=? AND id<? ORDER BY id DESC LIMIT 100`,
	ViewLogs:              `SELECT id,user_id,message_id,message_text,risk_score,matched_rules,ai_result,decision,source,created_at FROM moderation_logs WHERE chat_id=? AND id<? ORDER BY id DESC LIMIT 100`,
	ViewUsers:             `SELECT u.user_id,u.username,u.display_name,m.role,m.joined_at,m.verified_at,m.left_at,m.message_count FROM group_members m JOIN users u ON u.user_id=m.user_id WHERE m.chat_id=? AND u.user_id<? AND (CAST(u.user_id AS CHAR)=? OR u.username LIKE ? OR u.display_name LIKE ?) ORDER BY u.user_id DESC LIMIT 100`,
	ViewPunishments:       `SELECT id,user_id,message_id,decision,source,actor_id,status,deleted,acted,last_error,created_at FROM punishments WHERE chat_id=? AND id<? ORDER BY id DESC LIMIT 100`,
	ViewVerifications:     `SELECT user_id,status,challenge_type,attempts,expires_at,verified_at,created_at FROM verification_sessions WHERE chat_id=? ORDER BY created_at DESC LIMIT 100`,
	ViewAudits:            `SELECT id,actor_id,action,old_value,new_value,created_at FROM admin_audits WHERE chat_id=? AND id<? ORDER BY id DESC LIMIT 100`,
	ViewDeadUpdates:       `SELECT update_id,partition_id,attempts,last_error,created_at FROM update_inbox WHERE status='dead' AND update_id<? ORDER BY update_id DESC LIMIT 100`,
	ViewUserVerifications: `SELECT status,challenge_type,attempts,expires_at,verified_at,created_at FROM verification_sessions WHERE chat_id=? AND user_id=? ORDER BY created_at DESC LIMIT 20`,
	ViewUserReviews:       `SELECT id,message_text,risk_score,decision,created_at FROM moderation_logs WHERE chat_id=? AND user_id=? ORDER BY id DESC LIMIT 20`,
	ViewUserPunishments:   `SELECT id,decision,status,last_error,created_at FROM punishments WHERE chat_id=? AND user_id=? ORDER BY id DESC LIMIT 20`,
	ViewGroupStats:        `SELECT (SELECT COUNT(*) FROM group_members WHERE chat_id=? AND left_at IS NULL) AS known_members,(SELECT COUNT(*) FROM moderation_logs WHERE chat_id=?) AS reviewed_messages,(SELECT COUNT(*) FROM punishments WHERE chat_id=? AND status='done') AS punishments`,
	ViewListSummary:       `SELECT user_id,username FROM list_entries WHERE chat_id=? AND kind=? ORDER BY id DESC LIMIT 21`,
	ViewListMenu:          `SELECT id,user_id,username FROM list_entries WHERE chat_id=? AND kind=? AND id<? ORDER BY id DESC LIMIT 9`,
}

func (s *Store) View(ctx context.Context, view View, args ...any) ([]map[string]any, error) {
	q, ok := viewSQL[view]
	if !ok {
		return nil, fmt.Errorf("unknown data view")
	}
	return s.Rows(ctx, q, args...)
}
