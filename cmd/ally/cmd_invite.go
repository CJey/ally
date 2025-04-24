package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"

	"github.com/gofrs/flock"
	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"

	pb "github.com/cjey/ally/proto"
)

func init() {
	var cmd = &cobra.Command{
		Use:   "invite {appname}",
		Run:   runInvite,
		Short: "Invite an app to be with ally",
		Long:  `Invite an app to be with ally`,

		PersistentPreRun: parseEnvConfig,
	}

	cmd.PersistentFlags().Bool("enable", false, "enable this app")
	cmd.PersistentFlags().String("app-family", "", "the app's family name")
	cmd.PersistentFlags().String("app-user", "", "start with this uid or user name")
	cmd.PersistentFlags().String("app-group", "", "start with this gid or group name")
	cmd.PersistentFlags().String("app-bin", "", "the app's program file path")
	cmd.PersistentFlags().String("app-pwd", "", "the app's working directory")
	cmd.PersistentFlags().String("app-path", "", "the app's PATH")
	cmd.PersistentFlags().StringSlice("app-env", []string{}, "the app's ENV")

	RootCommand.AddCommand(cmd)
}

func runInvite(cmd *cobra.Command, args []string) {
	L.Printf("cmd: %s", shellquote.Join(os.Args...))
	if len(args) < 1 {
		Exit(1, "ERROR[Ally]: Appname not given\n")
	}
	ensureSocksPath()

	var appname = args[0]
	if !IsGoodName(appname) {
		Exit(1, "ERROR[Ally]: Invalid appname format\n")
	} else if appname == ALL_FAMILY {
		Exit(1, "ERROR[Ally]: Reserved appname[%s]\n", ALL_FAMILY)
	}

	// --enable
	var enable, _ = cmd.PersistentFlags().GetBool("enable")

	// --app-family
	var app_family string
	if val, _ := cmd.PersistentFlags().GetString("app-family"); val != "" {
		if !IsGoodName(val) {
			Exit(1, "ERROR[Ally]: Invalid family format\n")
		} else if val == ALL_FAMILY {
			Exit(1, "ERROR[Ally]: Reserved family[%s]\n", ALL_FAMILY)
		}
	}

	// --app-user
	var app_user string
	if val, _ := cmd.PersistentFlags().GetString("app-user"); val != "" {
		if _, err := user.LookupId(val); err != nil {
			if _, err := user.Lookup(val); err != nil {
				Exit(1, "ERROR[Ally]: Invalid user id or name\n")
			}
		}
		app_user = val
	}

	// --app-group
	var app_group string
	if val, _ := cmd.PersistentFlags().GetString("app-group"); val != "" {
		if _, err := user.LookupGroupId(val); err != nil {
			if _, err := user.LookupGroup(val); err != nil {
				Exit(1, "ERROR[Ally]: Invalid group id or name\n")
			}
		}
		app_group = val
	}

	// --app-bin
	var app_bin = appname
	if val, _ := cmd.PersistentFlags().GetString("app-bin"); val != "" {
		app_bin = val
	}
	if abs, err := LookPath(app_bin); err != nil {
		Exit(2, "ERROR[Ally]: Lookup binary path failed, %s\n", err)
	} else {
		app_bin = abs
	}

	// --app-pwd
	var app_pwd string
	if val, _ := cmd.PersistentFlags().GetString("app-pwd"); val != "" {
		app_pwd = val
	}
	// --app-path
	var app_path string
	if val, _ := cmd.PersistentFlags().GetString("app-path"); val != "" {
		app_path = val
	}
	// --app-env
	var app_envs, _ = cmd.PersistentFlags().GetStringSlice("app-env")

	lock := GetFlock()
	if err := lock.Lock(); err != nil {
		Exit(3, "ERROR[Ally]: Get db lock failed, %s\n", err)
	}
	cfgs, err := LoadDB()
	if err != nil {
		Exit(3, "ERROR[Ally]: Load db file failed, %s\n", err)
	}
	found := false
	for _, cfg := range cfgs {
		if cfg.Name == appname {
			cfg.Family = app_family
			cfg.Enable = enable
			cfg.User = app_user
			cfg.Group = app_group
			cfg.Bin = app_bin
			cfg.Pwd = app_pwd
			cfg.Path = app_path
			cfg.Envs = app_envs
			cfg.Args = args[1:]
			found = true
			break
		}
	}
	if !found {
		cfgs = append(cfgs, &AppConfig{
			Name:   appname,
			Family: app_family,
			Enable: enable,
			User:   app_user,
			Group:  app_group,
			Bin:    app_bin,
			Pwd:    app_pwd,
			Path:   app_path,
			Envs:   app_envs,
			Args:   args[1:],
		})
	}
	if err := SaveDB(cfgs); err != nil {
		Exit(4, "ERROR[Ally]: Save db file failed, %s\n", err)
	}
	lock.Unlock()

	apps := findApp(appname, 2, true, nil)
	statusApp(apps)
}

type AppConfig struct {
	Name   string   `json:"name"`
	Family string   `json:"family"`
	Enable bool     `json:"enable"`
	User   string   `json:"user"`
	Group  string   `json:"group"`
	Bin    string   `json:"bin"`
	Pwd    string   `json:"pwd"`
	Path   string   `json:"path"`
	Envs   []string `json:"envs"`
	Args   []string `json:"args"`
}

func GetFlock() *flock.Flock {
	db_file := filepath.Join(SocksPath, "db.json")
	return flock.New(db_file)
}

func LoadDB() ([]*AppConfig, error) {
	db_file := filepath.Join(SocksPath, "db.json")
	body, err := os.ReadFile(db_file)
	if err != nil {
		return nil, err
	}
	body = bytes.TrimSpace(body)
	var cfgs = []*AppConfig{}
	if len(body) == 0 {
		return cfgs, nil
	}

	if err := json.Unmarshal(body, &cfgs); err != nil {
		return nil, err
	}

	var idx_cfgs = make(map[string]*AppConfig)
	for _, cfg := range cfgs {
		if !IsGoodName(cfg.Name) {
			return nil, fmt.Errorf("invalid app name [%s] in db file", cfg.Name)
		}
		if cfg.Family != "" && !IsGoodName(cfg.Family) {
			return nil, fmt.Errorf("invalid family name [%s] in db file", cfg.Family)
		}
		if idx_cfgs[cfg.Name] != nil {
			return nil, fmt.Errorf("duplicate app name [%s] in db file", cfg.Name)
		} else {
			idx_cfgs[cfg.Name] = cfg
		}
	}
	sort.Slice(cfgs, func(i, j int) bool {
		return cfgs[i].Name < cfgs[j].Name
	})

	return cfgs, nil
}

func SaveDB(cfgs []*AppConfig) error {
	var idx_cfgs = make(map[string]*AppConfig)
	for _, cfg := range cfgs {
		if !IsGoodName(cfg.Name) {
			return fmt.Errorf("invalid app name [%s] to save", cfg.Name)
		}
		if cfg.Family != "" && !IsGoodName(cfg.Family) {
			return fmt.Errorf("invalid family name [%s] to save", cfg.Family)
		}
		if idx_cfgs[cfg.Name] != nil {
			return fmt.Errorf("duplicate app name [%s] to save", cfg.Name)
		} else {
			idx_cfgs[cfg.Name] = cfg
		}
	}

	sort.Slice(cfgs, func(i, j int) bool {
		return cfgs[i].Name < cfgs[j].Name
	})

	db_file := filepath.Join(SocksPath, "db.json")
	body, err := json.MarshalIndent(cfgs, "", "   ")
	if err != nil {
		return err
	}
	if err := os.Chmod(db_file, 0644); err != nil {
		return err
	}
	return os.WriteFile(db_file, body, 0644)
}

func (cfg *AppConfig) CommandLine() []string {
	args := []string{os.Args[0], "with", cfg.Name}
	if cfg.Bin != "" {
		args = append(args, "--app-bin", cfg.Bin)
	}
	if cfg.Family != "" {
		args = append(args, "--app-family", cfg.Family)
	}
	if cfg.User != "" {
		args = append(args, "--app-user", cfg.User)
	}
	if cfg.Group != "" {
		args = append(args, "--app-group", cfg.Group)
	}
	if cfg.Pwd != "" {
		args = append(args, "--app-pwd", cfg.Pwd)
	}
	if cfg.Path != "" {
		args = append(args, "--app-path", cfg.Path)
	}
	for _, env := range cfg.Envs {
		args = append(args, "--app-env", env)
	}
	if len(cfg.Args) > 0 {
		args = append(args, "--")
		args = append(args, cfg.Args...)
	}
	return args
}

func (cfg *AppConfig) String() string {
	args := cfg.CommandLine()
	return shellquote.Join(args...)
}

func ExtractConfigFromApp(app *pb.GetAppInfoResponse) *AppConfig {
	return &AppConfig{
		Name:   app.Info.Name,
		Family: app.Info.Family,
		User:   app.Info.User,
		Group:  app.Info.Group,
		Bin:    app.Info.Bin,
		Pwd:    app.Info.Pwd,
		Path:   app.Info.Path,
		Envs:   app.Info.Envs,
		Args:   app.Info.Args,
		Enable: false,
	}
}

func ConvertConfigToApp(cfg *AppConfig) *pb.GetAppInfoResponse {
	return &pb.GetAppInfoResponse{
		Info: &pb.AppInfo{
			Name:      cfg.Name,
			Family:    cfg.Family,
			User:      cfg.User,
			Group:     cfg.Group,
			Bin:       cfg.Bin,
			Pwd:       cfg.Pwd,
			Path:      cfg.Path,
			Envs:      cfg.Envs,
			Args:      cfg.Args,
			Stat:      pb.AppStat_STOPPED,
			Enable:    cfg.Enable,
			Ephemeral: false,
		},
	}
}
