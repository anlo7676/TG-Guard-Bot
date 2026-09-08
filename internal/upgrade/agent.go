package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// RunAgent must run on the host, separately from the application being replaced.
func RunAgent(ctx context.Context, project string) error {
	if !filepath.IsAbs(project) || project == string(filepath.Separator) {
		return errors.New("invalid project directory")
	}
	q := Queue{Dir: filepath.Join(project, ".updates")}
	root, err := os.OpenRoot(q.Dir)
	if err != nil {
		return err
	}
	defer root.Close()
	// Never replay an interrupted update automatically: its external effect is unknown.
	if _, e := root.Stat("active.json"); e == nil {
		if e = write(root, "status.json", Job{Phase: "failed", Message: "上次升级被中断，请检查运行版本和服务状态后重试", Updated: time.Now().UTC()}); e != nil {
			return e
		}
		if e = root.Remove("active.json"); e != nil {
			return e
		}
	}
	if _, e := root.Stat("request.json"); errors.Is(e, os.ErrNotExist) {
		_ = root.Remove("busy")
	}
	pulse := func() error { return write(root, "heartbeat", time.Now().UTC()) }
	if err = pulse(); err != nil {
		return err
	}
	beats, stop := context.WithCancel(ctx)
	defer stop()
	finished := make(chan struct{})
	defer func() { stop(); <-finished }()
	go func() {
		defer close(finished)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-beats.Done():
				return
			case <-ticker.C:
				if e := pulse(); e != nil {
					slog.Error("update heartbeat failed", "error", e)
				}
			}
		}
	}()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if e := root.Rename("request.json", "active.json"); errors.Is(e, os.ErrNotExist) {
				continue
			} else if e != nil {
				return e
			}
			if e := process(ctx, root, func(ctx context.Context) (Release, error) { return Latest(ctx, &http.Client{Timeout: 8 * time.Second}) }, func(ctx context.Context, version string) error {
				c, cancel := context.WithTimeout(ctx, 20*time.Minute)
				defer cancel()
				// No request can choose a URL, script path or shell argument.
				command := exec.CommandContext(c, "bash", filepath.Join(project, "install.sh"), "--deploy")
				command.Dir = project
				command.Env = append(os.Environ(), "TG_GUARD_INSTALL_DIR="+project, "TG_GUARD_TARGET_RELEASE="+version, "TG_WEB_UPDATE_JOB=1")
				command.Stdin = nil
				command.Stdout = io.Discard
				command.Stderr = io.Discard
				boundCommand(command)
				return command.Run()
			}); e != nil {
				return e
			}
		}
	}
}

func process(ctx context.Context, root *os.Root, latest func(context.Context) (Release, error), run func(context.Context, string) error) error {
	var job Job
	f, e := root.Open("active.json")
	if e != nil {
		return e
	}
	e = json.NewDecoder(io.LimitReader(f, 4096)).Decode(&job)
	f.Close()
	if e != nil || !versionPattern.MatchString(job.Version) {
		job = Job{Phase: "failed", Message: "升级请求无效"}
	} else {
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		release, err := latest(c)
		cancel()
		if err != nil || release.Version != job.Version {
			job.Phase = "failed"
			job.Message = "版本校验失败或稳定版已变化，请重新检查更新"
		} else {
			job.Phase = "running"
			job.Message = "正在下载并启动新版本，后台可能短暂断开"
			job.Updated = time.Now().UTC()
			if e = write(root, "active.json", job); e != nil {
				return e
			}
			if err = run(ctx, job.Version); err != nil {
				slog.Error("host upgrade command failed", "version", job.Version, "error", err)
				job.Phase = "failed"
				job.Message = "升级未完成，请检查服务器状态；启动失败时安装流程会尝试恢复原服务"
			} else {
				job.Phase = "succeeded"
				job.Message = "升级完成，服务已通过健康检查"
			}
		}
	}
	job.Updated = time.Now().UTC()
	if e = write(root, "status.json", job); e != nil {
		return e
	}
	if e = root.Remove("active.json"); e != nil {
		return e
	}
	return root.Remove("busy")
}
