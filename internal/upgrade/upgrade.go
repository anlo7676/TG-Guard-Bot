// Package upgrade exchanges constrained release requests with a host update agent.
package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const LatestURL = "https://api.github.com/repos/anlo7676/TG-Guard-Bot/releases/latest"

var versionPattern = regexp.MustCompile(`^v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$`)
var ErrBusy = errors.New("已有升级任务，请等待其完成")

type Release struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

func Newer(next, current string) bool {
	if !versionPattern.MatchString(next) || !versionPattern.MatchString("v"+strings.TrimPrefix(current, "v")) {
		return false
	}
	a, b := strings.Split(next[1:], "."), strings.Split(strings.TrimPrefix(current, "v"), ".")
	for i := range a {
		x, _ := strconv.Atoi(a[i])
		y, _ := strconv.Atoi(b[i])
		if x != y {
			return x > y
		}
	}
	return false
}
func Latest(ctx context.Context, client *http.Client) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", LatestURL, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, errors.New("暂时无法连接版本服务，请稍后重试")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Release{}, fmt.Errorf("版本服务暂不可用（HTTP %d）", resp.StatusCode)
	}
	var v struct {
		Tag     string `json:"tag_name"`
		Draft   bool   `json:"draft"`
		Preview bool   `json:"prerelease"`
		Assets  []struct {
			Name string `json:"name"`
		} `json:"assets"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&v); err != nil || !versionPattern.MatchString(v.Tag) || v.Draft || v.Preview {
		return Release{}, errors.New("未取得有效的稳定版本")
	}
	names := map[string]bool{}
	for _, a := range v.Assets {
		names[a.Name] = true
	}
	if !names["SHA256SUMS"] || !names["tgguard-linux-amd64"] || !names["tgguard-linux-arm64"] {
		return Release{}, errors.New("稳定版本附件尚未齐全")
	}
	return Release{v.Tag, "https://github.com/anlo7676/TG-Guard-Bot/releases/tag/" + v.Tag}, nil
}

type Job struct {
	Version string    `json:"version"`
	Phase   string    `json:"phase"`
	Message string    `json:"message"`
	Updated time.Time `json:"updated_at,omitzero"`
}
type Queue struct{ Dir string }

func (q Queue) Ready() bool {
	if q.Dir == "" {
		return false
	}
	root, e := os.OpenRoot(q.Dir)
	if e != nil {
		return false
	}
	defer root.Close()
	st, e := root.Stat("heartbeat")
	return e == nil && time.Since(st.ModTime()) < 20*time.Second
}
func (q Queue) Status() Job {
	j := Job{Phase: "idle", Message: "暂无升级任务"}
	root, e := os.OpenRoot(q.Dir)
	if e != nil {
		return j
	}
	defer root.Close()
	for _, name := range []string{"active.json", "request.json", "status.json"} {
		f, e := root.Open(name)
		if e != nil {
			continue
		}
		e = json.NewDecoder(io.LimitReader(f, 4096)).Decode(&j)
		f.Close()
		if e == nil {
			return j
		}
	}
	return j
}
func (q Queue) Request(version string) error {
	if !versionPattern.MatchString(version) {
		return errors.New("版本格式无效")
	}
	if !q.Ready() {
		return errors.New("服务器升级服务未启用，请先在服务器管理菜单启用网页升级")
	}
	root, e := os.OpenRoot(q.Dir)
	if e != nil {
		return e
	}
	defer root.Close()
	f, e := root.OpenFile("busy", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(e, os.ErrExist) {
		return ErrBusy
	}
	if e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	e = write(root, "request.json", Job{Version: version, Phase: "queued", Message: "等待服务器开始升级", Updated: time.Now().UTC()})
	if e != nil {
		_ = root.Remove("busy")
	}
	return e
}
func write(root *os.Root, name string, v any) error {
	f, e := root.OpenFile(name+".tmp", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if e != nil {
		return e
	}
	e = json.NewEncoder(f).Encode(v)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return root.Rename(name+".tmp", name)
}
