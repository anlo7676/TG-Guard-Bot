package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"tgguard/internal/domain"
	"tgguard/internal/rules"
	"tgguard/internal/state"
	"tgguard/internal/store"
)

type profileData struct {
	Bio             string
	BioAvailable    bool
	AvatarHash      string
	AvatarAvailable bool
}

type ProfileResult struct {
	UserID          int64  `json:"user_id"`
	Matched         bool   `json:"matched"`
	Reason          string `json:"reason,omitempty"`
	Domain          string `json:"domain,omitempty"`
	AvatarHash      string `json:"avatar_hash,omitempty"`
	BioAvailable    bool   `json:"bio_available"`
	AvatarAvailable bool   `json:"avatar_available"`
	Present         bool   `json:"present"`
	Protected       bool   `json:"protected"`
	Learned         bool   `json:"learned"`
	Applied         bool   `json:"applied"`
	Status          string `json:"status,omitempty"`
	bio             string
}

func (s *Service) inspectProfile(ctx context.Context, chat, user int64, settings domain.Settings, fresh, learn bool) (ProfileResult, error) {
	r := ProfileResult{UserID: user}
	key := fmt.Sprintf("profile:v1:%d:%t", user, settings.ProfileAvatarMatch)
	var data profileData
	if fresh || s.State.Get(ctx, key, &data) != nil {
		allowed, err := s.State.Limit(ctx, "profile:rate", 4, time.Second)
		if err != nil {
			return r, err
		}
		if !allowed {
			return r, errors.New("profile inspection rate limited")
		}
		fetch, cancel := context.WithTimeout(ctx, 8*time.Second)
		p := s.Bot.Profile(fetch, user, settings.ProfileAvatarMatch)
		cancel()
		data = profileData{Bio: p.Bio, BioAvailable: p.BioAvailable, AvatarAvailable: p.PhotoAvailable}
		if len(p.Photo) > 0 {
			data.AvatarHash, err = rules.AvatarHash(p.Photo)
			if err != nil {
				data.AvatarAvailable = false
			}
		}
		ttl := 5 * time.Minute
		if !data.BioAvailable || settings.ProfileAvatarMatch && !data.AvatarAvailable {
			ttl = time.Minute
		}
		if err := s.State.Put(ctx, key, data, ttl); err != nil {
			return r, err
		}
	}
	r.BioAvailable, r.AvatarAvailable, r.AvatarHash, r.bio = data.BioAvailable, data.AvatarAvailable, data.AvatarHash, data.Bio
	r.Domain, r.Reason = rules.ProfileMatch(data.Bio, settings.ProfileBlockedDomains)
	if r.Domain != "" {
		r.Matched = true
		if learn && settings.ProfileAvatarMatch && data.AvatarHash != "" {
			protected, err := s.Protected(ctx, chat, domain.User{ID: user})
			if err != nil {
				return r, err
			}
			if protected {
				return r, nil
			}
			if err := s.Store.LearnProfileAvatar(ctx, chat, user, data.AvatarHash, r.Domain); err != nil {
				return r, err
			}
			r.Learned = true
		}
	} else if settings.ProfileAvatarMatch && data.AvatarHash != "" {
		d, err := s.Store.ProfileAvatarDomain(ctx, chat, data.AvatarHash)
		if err != nil {
			return r, err
		}
		// Removing a domain also deactivates samples learned from that domain.
		if d != "" && slices.Contains(settings.ProfileBlockedDomains, d) {
			r.Matched, r.Domain, r.Reason = true, d, "profile_avatar:"+data.AvatarHash
		}
	}
	return r, nil
}

func (s *Service) profileLog(ctx context.Context, chat, user, message int64, key string, r ProfileResult) (store.Log, error) {
	return s.Store.SaveLog(ctx, store.Log{EventKey: key, ChatID: chat, UserID: user, MessageID: message,
		Text: r.bio, Risk: domain.Risk{Score: 100, Matches: []domain.Match{{Rule: "profile_guard", Score: 100, Reason: r.Reason}}},
		Decision: domain.Decision{Action: "ban", Reason: r.Reason}, Source: "profile_guard"})
}

// ProfileCheck is used by bounded administrative scans. Preview never writes
// samples or punishments; learn seeds only confirmed domain/contact avatars.
func (s *Service) ProfileCheck(ctx context.Context, chat, user int64, apply, learn bool, actor int64) (ProfileResult, error) {
	r := ProfileResult{UserID: user}
	if user <= 0 {
		return r, errors.New("invalid user ID")
	}
	ok, err := s.Store.GroupAuthorized(ctx, chat)
	if err != nil {
		return r, err
	}
	if !ok {
		return r, errors.New("group not authorized")
	}
	settings, err := s.Store.Settings(ctx, chat)
	if err != nil {
		return r, err
	}
	if !settings.ProfileGuardEnabled {
		return r, errors.New("profile guard is disabled")
	}
	m, err := s.Bot.Member(ctx, chat, user)
	if err != nil {
		return r, err
	}
	r.Present = m.Present()
	protected, err := s.Protected(ctx, chat, m.User)
	if err != nil {
		return r, err
	}
	if protected {
		r.Protected = true
		return r, nil
	}
	r, err = s.inspectProfile(ctx, chat, user, settings, true, learn)
	r.Present = m.Present()
	if err != nil || !apply || !r.Matched || !r.Present {
		return r, err
	}
	token, err := state.Token()
	if err != nil {
		return r, err
	}
	l, err := s.profileLog(ctx, chat, user, 0, "profile-scan:"+token, r)
	if err != nil {
		return r, err
	}
	if err = s.Punish(ctx, l, actor); err != nil {
		return r, err
	}
	r.Status, err = s.Store.ProfilePunishmentStatus(ctx, l.EventKey)
	r.Applied = r.Status == "done"
	return r, err
}

// Automatic profile failures do not block verification or ordinary moderation.
// Available positive evidence still wins when another profile field is hidden.
func (s *Service) automaticProfile(ctx context.Context, chat, user int64, settings domain.Settings, fresh bool) (ProfileResult, bool) {
	if !settings.ProfileGuardEnabled {
		return ProfileResult{}, false
	}
	r, err := s.inspectProfile(ctx, chat, user, settings, fresh, true)
	if err != nil {
		slog.Warn("profile inspection deferred", "chat_id", chat, "user_id", user, "error", err)
		return r, false
	}
	return r, r.Matched
}
