package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/rpmpack"
	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"
)

var (
	Root     string // current directory
	PJRoot   string // project directory
	Hostname string // current hostname

	Appname     string
	Version     string
	Release     string
	FullVersion string

	TargetFile string
	CmdFile    string
	DataDir    string
	BinFile    string
	LogDir     string
)

func exit(code int, format string, a ...any) {
	fmt.Fprintf(os.Stderr, format, a...)
	os.Exit(code)
}

func init() {
	Prepare()
}

func main() {
	var cmd = &cobra.Command{
		Use:    `rpmbuild`,
		PreRun: ParseFlags,
		Run:    Run,
		Short:  fmt.Sprintf(`For build rpm package of app [%s] only`, os.Getenv("APPNAME")),
		Long:   fmt.Sprintf(`For build rpm package of app [%s] only`, os.Getenv("APPNAME")),
	}
	DefineFlags(cmd)
	cmd.Execute()
}

func DefineFlags(cmd *cobra.Command) {
	cmd.Flags().String("arch", "all", "target cpu arch")
}

func ParseFlags(cmd *cobra.Command, args []string) {
	f_arch, _ := cmd.Flags().GetString("arch")
	switch strings.ToLower(f_arch) {
	case "386", "x86", "i386", "i686":
		f_arch = "386"
	case "amd64", "x64", "x86_64":
		f_arch = "amd64"
	case "arm", "aarch":
		f_arch = "arm"
	case "arm64", "aarch64":
		f_arch = "arm64"
	case "all", "noarch", "any":
		f_arch = ""
	default:
		exit(100, "ERROR: unsupport arch[%s]\n", f_arch)
	}
	if err := os.Setenv("GOARCH", f_arch); err != nil {
		exit(101, "ERROR: set environ GOARCH failed, %s\n", err)
	}

	TargetFile = fmt.Sprintf("%s-%s.%s.rpm", Appname, FullVersion, RPMArch(f_arch))
}

func Run(cmd *cobra.Command, args []string) {
	md := rpmpack.RPMMetaData{
		Name:        Appname,
		Group:       "Unspecified",
		Version:     Version,
		Release:     Release,
		Arch:        RPMArch(os.Getenv("GOARCH")),
		OS:          os.Getenv("GOOS"),
		Packager:    "cjey.hou@gmail.com",
		Vendor:      "CJey",
		Licence:     "MIT",
		BuildTime:   time.Now(),
		BuildHost:   Hostname,
		Epoch:       0, // always 0
		URL:         os.Getenv("GIT_REPO"),
		Summary:     "I am your ally",
		Description: "Ally supply a good method to reload golang app gracefully",
	}
	if err := md.Requires.Set("logrotate"); err != nil {
		panic(err)
	}
	rpm, err := rpmpack.NewRPM(md)
	if err != nil {
		panic(err)
	}

	// build binary
	var archs = []string{"386", "amd64", "arm", "arm64"}
	if arch := os.Getenv("GOARCH"); arch != "" {
		archs = []string{arch}
	}
	var binfiles = make(map[string]string)
	for _, arch := range archs {
		var pattern = fmt.Sprintf("%s.%s.rpmbuild.*", Appname, arch)
		ts := time.Now()
		fmt.Printf("> Buiding... GOOS=%s GOARCH=%s\n", os.Getenv("GOOS"), arch)
		fmt.Printf("  App:     %s\n", Appname)
		fmt.Printf("  Version: %s\n", FullVersion)
		if f, err := os.CreateTemp("", pattern); err != nil {
			exit(103, "ERROR: create temp build file failed, %s\n", err)
		} else {
			binfiles[arch] = f.Name()
			f.Close()
			defer os.Remove(binfiles[arch])
		}
		buildcmd := shellquote.Join("MAINFILE="+PJRoot+"/cmd/"+Appname+"/*.go", Root+"/build",
			Appname, binfiles[arch])
		out, err := Shell(buildcmd, "GOARCH="+arch)
		if err != nil {
			exit(104, "ERROR: build app[%s] failed, %s\n\n%s\n\n%s\n", Appname, err, buildcmd, out)
		}
		fmt.Printf("  Elapsed: %s\n\n", time.Since(ts).Truncate(1*time.Millisecond))
	}

	// handle files
	ts := time.Now()
	fmt.Printf("> Packaging...\n")
	fmt.Printf("  Target:  %s\n", TargetFile)
	// 1. dir
	rpm.AddFile(RPMDir(DataDir))
	rpm.AddFile(RPMDir(LogDir))
	// 2. config
	if f := RPMFileFrom(Root+"/logrotate.conf", "/etc/logrotate.d/"+Appname); true {
		f.Body = []byte(EnvReplace(string(f.Body)))
		f.Type = rpmpack.ConfigFile | rpmpack.NoReplaceFile
		f.Mode = 0100000 | 0644
		rpm.AddFile(f)
	}
	// 3. regular
	for arch, binfile := range binfiles {
		// real bin with arch suffix
		f := RPMFileFrom(binfile, BinFile+"."+RPMArch(arch))
		f.Mode |= 0775
		rpm.AddFile(f)
	}

	if f := RPMFileFrom(Root+"/ally-wakeup.service",
		DataDir+"/ally-wakeup.service"); true {
		f.Body = []byte(EnvReplace(string(f.Body)))
		rpm.AddFile(f)
	}
	if f := RPMFileFrom(Root+"/ally-wakeup",
		DataDir+"/ally-wakeup"); true {
		f.Body = []byte(EnvReplace(string(f.Body)))
		f.Mode |= 0775
		rpm.AddFile(f)
	}

	// scriptlets
	rpm.AddPrein(EnvReplace(`
if [ $1 -eq 1 ]; then # 首次安装
    mkdir -p /etc/logrotate.d
fi
`))

	rpm.AddPostin(EnvReplace(`
arch="$(lscpu | grep '^Architecture:' | awk '{print $2}')"
[ "$arch" = "i386" -o "$arch" = "i686" ] && arch="i386"
[ "$arch" = "x86_64" ] && arch="x86_64"
[ "$arch" = "armv7l" ] && arch="armv7hl"
[ "$arch" = "aarch64" ] && arch="aarch64"
ln -sfT ${{BinFile}}.$arch ${{CmdFile}}

if [ -d /run/systemd/system ]; then
    rm -f /usr/lib/systemd/system/ally-wakeup.service
    cp ${{DataDir}}/ally-wakeup.service /usr/lib/systemd/system/ally-wakeup.service
    systemctl daemon-reload
    systemctl disable ally-wakeup
    systemctl enable ally-wakeup
else
    rm -f /etc/init.d/ally-wakeup
    cp ${{DataDir}}/ally-wakeup /etc/init.d/ally-wakeup
    chkconfig --add ally-wakeup
fi
`))

	rpm.AddPreun(EnvReplace(`
`))

	rpm.AddPostun(EnvReplace(`
if [ $1 -eq 0 ]; then # 卸载
    rm -f ${{CmdFile}}
    rm -f /usr/lib/systemd/system/ally-wakeup.service \
          /etc/systemd/system/ally-wakeup.service \
          /etc/systemd/system/multi-user.target.wants/ally-wakeup.service \
          /etc/init.d/ally-wakeup
fi
`))

	// write rpm
	target, err := os.Create(TargetFile)
	if err != nil {
		exit(104, "ERROR: create rpm file[%s] failed, %s\n", TargetFile, err)
	}
	defer target.Close()
	if err := rpm.Write(target); err != nil {
		exit(104, "ERROR: make rpm file[%s] failed, %s\n", TargetFile, err)
	}
	fmt.Printf("  Elapsed: %s\n\n", time.Since(ts).Truncate(1*time.Millisecond))

	fmt.Printf("> Done!\n")
}

func Exec(script string, envs ...string) (string, int, error) {
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), envs...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0, nil
	}
	if e, ok := err.(*exec.ExitError); ok {
		return string(out), e.ExitCode(), nil
	} else {
		return "", 0, err
	}
}

func Shell(script string, envs ...string) (string, error) {
	if out, code, err := Exec(script, envs...); err == nil && code != 0 {
		return out, fmt.Errorf("exit code %d", code)
	} else {
		return out, err
	}
}

func Prepare() {
	// locate
	if p, e := os.Getwd(); e != nil {
		exit(100, "ERROR: get working directory failed, %s\n", e)
	} else if p, e := filepath.Abs(p); e != nil {
		exit(100, "ERROR: get working directory absolute path failed, %s\n", e)
	} else {
		Root = filepath.Dir(p)
		PJRoot = filepath.Dir(Root)
		Hostname, _ = os.Hostname()
	}

	// set env
	out, err := Shell(fmt.Sprintf("%s/build env", Root))
	if err != nil {
		exit(102, "ERROR: collect build env failed, %s\n", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Index(line, "=") < 0 {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) == 1 {
			kv = append(kv, "")
		}
		if err := os.Setenv(kv[0], kv[1]); err != nil {
			exit(101, "ERROR: set build environ failed, %s\n", err)
		}
	}
	Appname, FullVersion = os.Getenv("APPNAME"), os.Getenv("VERSION")
	if ss := strings.SplitN(FullVersion, "-", 2); len(ss) == 2 {
		Version, Release = ss[0], ss[1]
	}

	// path
	CmdFile = "/bin/" + Appname
	DataDir = "/var/lib/" + Appname
	BinFile = DataDir + "/" + Appname
	LogDir = "/var/log/" + Appname

	// force linux
	if err := os.Setenv("GOOS", "linux"); err != nil {
		exit(101, "ERROR: set environ GOOS failed, %s\n", err)
	}
}

func RPMDir(name string) rpmpack.RPMFile {
	return rpmpack.RPMFile{
		Name: name, Body: nil, MTime: uint32(time.Now().Unix()),
		Owner: "root", Group: "root", Mode: 040000 | 0775,
	}
}

func RPMSymlink(symbol, target string) rpmpack.RPMFile {
	return rpmpack.RPMFile{
		Name: target, Body: []byte(symbol), MTime: uint32(time.Now().Unix()),
		Owner: "root", Group: "root", Mode: 0120000 | 0777,
	}
}

func RPMFile(dst string, body []byte) rpmpack.RPMFile {
	return rpmpack.RPMFile{
		Name: dst, Body: body, MTime: uint32(time.Now().Unix()),
		Owner: "root", Group: "root", Mode: 0100000 | 0664,
	}
}

func RPMFileFrom(src, dst string) rpmpack.RPMFile {
	var f = RPMFile(dst, nil)
	if body, err := os.ReadFile(src); err != nil {
		exit(105, "ERROR: read file[%s] failed, %s\n", src, err)
	} else if stat, err := os.Stat(src); err != nil {
		exit(105, "ERROR: stat file[%s] failed, %s\n", src, err)
	} else {
		f.Body = body
		f.MTime = uint32(stat.ModTime().Unix())
	}
	return f
}

func EnvReplace(origin string) string {
	rp := strings.NewReplacer(
		"${{Appname}}", Appname,
		"${{CmdFile}}", CmdFile,
		"${{BinFile}}", BinFile,
		"${{DataDir}}", DataDir,
		"${{LogDir}}", LogDir,
	)
	return rp.Replace(origin)
}

func RPMArch(arch string) string {
	switch arch {
	case "386":
		return "i386"
	case "amd64":
		return "x86_64"
	case "arm":
		return "armv7hl"
	case "arm64":
		return "aarch64"
	default:
		return "noarch"
	}
}
