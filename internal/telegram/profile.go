package telegram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Profile struct {
	Bio            string
	BioAvailable   bool
	Photo          []byte
	PhotoAvailable bool
}

// Profile reads current visible data only. Failure of one field does not turn
// the other into a negative result; callers must preserve availability flags.
func (c *Client) Profile(ctx context.Context, user int64, avatar bool) Profile {
	var p Profile
	var chat struct {
		Bio   string `json:"bio"`
		Photo struct {
			FileID string `json:"big_file_id"`
		} `json:"photo"`
	}
	if c.Call(ctx, "getChat", map[string]any{"chat_id": user}, &chat) == nil {
		p.Bio, p.BioAvailable = chat.Bio, true
	}
	if !avatar {
		return p
	}
	fileID := chat.Photo.FileID
	if fileID == "" {
		var photos struct {
			Photos [][]struct {
				ID string `json:"file_id"`
			} `json:"photos"`
		}
		if c.Call(ctx, "getUserProfilePhotos", map[string]any{"user_id": user, "limit": 1}, &photos) != nil {
			return p
		}
		if len(photos.Photos) == 0 || len(photos.Photos[0]) == 0 {
			p.PhotoAvailable = true
			return p
		}
		fileID = photos.Photos[0][len(photos.Photos[0])-1].ID
	}
	var file struct {
		Path string `json:"file_path"`
		Size int64  `json:"file_size"`
	}
	if c.Call(ctx, "getFile", map[string]any{"file_id": fileID}, &file) != nil || file.Size > 2<<20 {
		return p
	}
	data, err := c.profileFile(ctx, file.Path)
	if err == nil {
		p.Photo, p.PhotoAvailable = data, true
	}
	return p
}

func (c *Client) profileFile(ctx context.Context, path string) ([]byte, error) {
	base, err := url.Parse(c.BaseURL)
	if err != nil || base.RawQuery != "" || base.Fragment != "" || !strings.HasPrefix(base.Path, "/bot") {
		return nil, errors.New("invalid profile endpoint")
	}
	if path == "" || strings.ContainsAny(path, "\\?#%") || strings.HasPrefix(path, "/") {
		return nil, errors.New("invalid profile file")
	}
	for _, part := range strings.Split(path, "/") {
		if part == ".." || part == "." || part == "" {
			return nil, errors.New("invalid profile file")
		}
	}
	base.Path = "/file" + base.Path + "/" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, errors.New("invalid profile request")
	}
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("profile download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("profile download rejected")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return nil, errors.New("profile download exceeds limit or failed")
	}
	return data, nil
}
