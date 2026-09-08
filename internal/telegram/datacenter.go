package telegram

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
)

var ErrDCUnavailable = errors.New("没有可见头像，或头像格式暂不支持")

// PhotoDC reads the bounded v2-v4 TDLib persistent file identifier header.
// This is a photo storage DC, not an account home DC or the user's location.
func PhotoDC(id string) (int, error) {
	if len(id) > 4096 {
		return 0, ErrDCUnavailable
	}
	packed, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return 0, ErrDCUnavailable
	}
	data := make([]byte, 0, len(packed))
	for i := 0; i < len(packed); i++ {
		if packed[i] != 0 {
			data = append(data, packed[i])
		} else {
			i++
			if i >= len(packed) || packed[i] == 0 {
				return 0, ErrDCUnavailable
			}
			data = append(data, make([]byte, int(packed[i]))...)
		}
		if len(data) > 8192 {
			return 0, ErrDCUnavailable
		}
	}
	if len(data) < 25 {
		return 0, ErrDCUnavailable
	}
	version := data[len(data)-1]
	if version < 2 || version > 4 {
		return 0, ErrDCUnavailable
	}
	kind := binary.LittleEndian.Uint32(data[:4])
	if kind & ^uint32(1<<25|3) != 0 || (kind & ^uint32(1<<25)) != 1 && (kind & ^uint32(1<<25)) != 2 {
		return 0, ErrDCUnavailable
	}
	dc := int(binary.LittleEndian.Uint32(data[4:8]))
	if dc < 1 || dc > 5 {
		return 0, ErrDCUnavailable
	}
	return dc, nil
}

func (c *Client) UserPhotoDC(ctx context.Context, user int64) (int, error) {
	var result struct {
		Photos [][]struct {
			FileID string `json:"file_id"`
		} `json:"photos"`
	}
	if err := c.Call(ctx, "getUserProfilePhotos", map[string]any{"user_id": user, "offset": 0, "limit": 1}, &result); err != nil {
		return 0, err
	}
	if len(result.Photos) > 0 {
		for _, p := range result.Photos[0] {
			if dc, err := PhotoDC(p.FileID); err == nil {
				return dc, nil
			}
		}
	}
	return 0, ErrDCUnavailable
}
