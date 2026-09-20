//go:build windows

package main

import (
	"context"
	"path/filepath"

	"github.com/ai-employee-platform/workstation/internal/config"
	"github.com/ai-employee-platform/workstation/internal/daemon"
	"github.com/ai-employee-platform/workstation/internal/platform"
	"golang.org/x/sys/windows/svc"
)

type aewWindowsService struct{}

func (s *aewWindowsService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	paths := platform.Detect()
	cfgPath := filepath.Join(paths.ConfigDir(), "config.yaml")
	cfg, _ := config.LoadFile(cfgPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := daemon.New(daemon.Options{
		Paths: paths, Config: cfg,
	})

	errChan := make(chan error, 1)
	go func() {
		errChan <- d.Run(ctx)
	}()

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	for {
		select {
		case req := <-r:
			switch req.Cmd {
			case svc.Interrogate:
				changes <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				<-errChan
				return false, 0
			}
		case <-errChan:
			return false, 0
		}
	}
}

func checkWindowsService() (bool, error) {
	isService, err := svc.IsWindowsService()
	if err != nil || !isService {
		return false, nil
	}
	err = svc.Run("AIEmployeeWorkstation", &aewWindowsService{})
	return true, err
}
