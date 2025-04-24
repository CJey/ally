package main

import (
	"fmt"
	"log"
	"log/syslog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/cjey/ally/release/app"
)

const (
	ALL_FAMILY = "all"
)

var (
	StartTime  = time.Now()
	IsTerminal = true
)

var (
	W           *syslog.Writer
	L           = log.Default()
	RefreshWait = 50 * time.Millisecond

	CrashWait = 3000 * time.Millisecond
	SocksPath = "/var/lib/ally"
	LogsPath  = "/var/log/ally"
)

var (
	ErrNotAlly = fmt.Errorf("not ally")
)

var RootCommand = &cobra.Command{
	Use:   app.Name,
	Short: `I am your ally`,
	Long: `Ally is a process manager which provide reload service gracefully
    
Config by env:
    ALLY_CONFIG_CRASH_WAIT=3000, wait some milliseconds then start a new app instance after app crashed
    ALLY_CONFIG_SOCKS_PATH=/var/lib/ally, ally's socks & storage directory
    ALLY_CONFIG_LOGS_PATH=/var/log/ally, ally's app stdout & stderr log directory
`,
}

func init() {
	if fileInfo, _ := os.Stdout.Stat(); (fileInfo.Mode() & os.ModeCharDevice) == 0 {
		IsTerminal = false
	}
	if w, err := syslog.New(syslog.LOG_INFO, app.Name); err == nil {
		W, L = w, log.New(w, "", 0)
	} else {
		L.Printf("WARN: %s, ignore", err)
	}
}

func main() {
	RootCommand.CompletionOptions.HiddenDefaultCmd = true
	if err := RootCommand.Execute(); err != nil {
		Exit(101, "%s", err.Error())
	}
}

func parseEnvConfig(*cobra.Command, []string) {
	if val := os.Getenv("ALLY_CONFIG_CRASH_WAIT"); val != "" {
		if v, e := strconv.ParseInt(val, 10, 64); e != nil {
			Exit(102, "ERROR[Ally]: ALLY_CONFIG_CRASH_WAIT must be a duration(ms), %s\n", e)
		} else if v > 0 {
			CrashWait = time.Duration(v) * time.Millisecond
		} else if v < 0 {
			CrashWait = 0
		}
	}
	if val := os.Getenv("ALLY_CONFIG_SOCKS_PATH"); val != "" {
		if val, err := filepath.Abs(val); err != nil {
			Exit(103, "ERROR[Ally]: Get socks directory fullpath failed, %s\n", err)
		} else {
			SocksPath = val
		}
	}
	if val := os.Getenv("ALLY_CONFIG_LOGS_PATH"); val != "" {
		if val, err := filepath.Abs(val); err != nil {
			Exit(103, "ERROR[Ally]: Get socks directory fullpath failed, %s\n", err)
		} else {
			LogsPath = val
		}
	}
}

func ensureSocksPath() {
	if _, err := os.Stat(SocksPath); os.IsNotExist(err) {
		if err := os.MkdirAll(SocksPath, 0755); err != nil {
			Exit(104, "ERROR[Ally]: Create socks directory failed, %s\n", err)
		}
	}
}
func ensureLogsPath() {
	if _, err := os.Stat(LogsPath); os.IsNotExist(err) {
		if err := os.MkdirAll(LogsPath, 0755); err != nil {
			Exit(104, "ERROR[Ally]: Create logs directory failed, %s\n", err)
		}
	}
}
