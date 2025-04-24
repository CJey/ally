package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"

	pb "github.com/cjey/ally/proto"
)

func init() {
	var cmd = &cobra.Command{
		Use:   "start {app}",
		Run:   runStart,
		Short: "Start an app with ally",
		Long:  `Start an app with ally`,

		PersistentPreRun: parseEnvConfig,
	}

	cmd.PersistentFlags().Bool("pid", false, "found app by pid")
	cmd.PersistentFlags().Bool("name", false, "found app by name")
	cmd.PersistentFlags().Bool("family", false, "found app by family")

	RootCommand.AddCommand(cmd)
}

func runStart(cmd *cobra.Command, args []string) {
	L.Printf("cmd: %s", shellquote.Join(os.Args...))
	if len(args) < 1 {
		Exit(1, "ERROR[Ally]: App not given\n")
	}
	ensureLogsPath()

	var _, apps, all = FindApp(cmd, args, false)
	var stopped []*pb.GetAppInfoResponse
	var idx_all = make(map[string][]*pb.GetAppInfoResponse)
	for _, app := range all {
		name := app.Info.Name
		idx_all[name] = append(idx_all[name], app)
	}
	for _, app := range apps {
		name := app.Info.Name
		if len(idx_all[name]) > 1 {
			Exit(2, "ERROR[Ally]: App name [%s] confusing among %d apps\n", name, len(idx_all[name]))
		}
		if app.Pid == 0 {
			stopped = append(stopped, app)
		}
	}

	for _, app := range stopped {
		if err := StartApp(app); err != nil {
			Exit(3, "ERROR[Ally]: Start app [%s] failed, %s\n", app.Info.Name, err)
		}
	}

	time.Sleep(RefreshWait)

	_, apps, _ = FindApp(cmd, args, false)
	listApp(apps)
}

func StartApp(app *pb.GetAppInfoResponse) error {
	info := app.Info
	args := ExtractConfigFromApp(app).CommandLine()

	file, err := os.OpenFile(fmt.Sprintf("%s/%s.log", LogsPath, info.Name), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open stdout & stderr log file failed, %w", err)
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	cmd.Stdin = nil
	cmd.Stdout = file
	cmd.Stderr = file

	// let all fds not be inherited
	filepath.WalkDir(fmt.Sprintf("/proc/%d/fd", os.Getpid()),
		func(_ string, d fs.DirEntry, _ error) error {
			if fd, _ := strconv.ParseUint(d.Name(), 10, 64); fd > 2 {
				if val, err := unix.FcntlInt(uintptr(fd), syscall.F_GETFD, 0); err == nil {
					if (val & syscall.FD_CLOEXEC) == 0 {
						unix.FcntlInt(uintptr(fd), syscall.F_SETFD, val|syscall.FD_CLOEXEC)
					}
				}
			}
			return nil
		})

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start command failed, %w", err)
	}

	sock := fmt.Sprintf("%s/%d.sock", SocksPath, cmd.Process.Pid)
	for i := 0; i < 10; i++ {
		time.Sleep(10 * time.Millisecond)
		if _, err := GetAllyAppInfo(sock); err == nil {
			return nil
		}
	}
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		if _, err := GetAllyAppInfo(sock); err == nil {
			return nil
		}
	}
	return fmt.Errorf("cannot attached to [%s]'s ally", info.Name)
}
