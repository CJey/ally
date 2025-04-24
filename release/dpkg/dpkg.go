package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cjey/debpkg"
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
		Use:    `dpkgbuild`,
		PreRun: ParseFlags,
		Run:    Run,
		Short:  fmt.Sprintf(`For build deb package of app [%s] only`, os.Getenv("APPNAME")),
		Long:   fmt.Sprintf(`For build deb package of app [%s] only`, os.Getenv("APPNAME")),
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

	TargetFile = fmt.Sprintf("%s-%s-%s.deb", Appname, FullVersion, DEBArch(f_arch))
}

func Run(cmd *cobra.Command, args []string) {
	pkg := debpkg.New()
	pkg.SetName(Appname)
	pkg.SetVersion(Version)
	pkg.SetArchitecture(DEBArch(os.Getenv("GOARCH")))
	pkg.SetMaintainer("CJey Hou")
	pkg.SetMaintainerEmail("cjey.hou@gmail.com")
	pkg.SetSection("admin")
	pkg.SetDepends("logrotate,rsyslog")
	pkg.SetVcsType(debpkg.VcsTypeGit)
	pkg.SetVcsURL(os.Getenv("GIT_REPO"))
	pkg.SetHomepage("https://cjey.me")
	pkg.SetShortDescription("Ally supply a good method to reload golang app gracefully")

	// build binary
	var archs = []string{"386", "amd64", "arm", "arm64"}
	if arch := os.Getenv("GOARCH"); arch != "" {
		archs = []string{arch}
	}
	var binfiles = make(map[string]string)
	for _, arch := range archs {
		var pattern = fmt.Sprintf("%s.%s.debbuild.*", Appname, arch)
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
	// 1. ops files
	AddEmptyDirectory(pkg, LogDir)
	AddFileString(pkg, ReadFile(Root+"/logrotate.conf"), "/etc/logrotate.d/"+Appname)
	AddFileString(pkg, ReadFile(Root+"/ally-wakeup"), DataDir+"/ally-wakeup")
	AddFileString(pkg, ReadFile(Root+"/ally-wakeup.service"), DataDir+"/ally-wakeup.service")
	// 2. bin files
	for arch, binfile := range binfiles {
		AddFile(pkg, binfile, BinFile+"."+DEBArch(arch))
	}
	// 3. config files
	AddFileString(pkg, "", DataDir+"/db.json")
	pkg.MarkConfigFile(DataDir + "/db.json")

	// 4. control files
	AddControlExtraString(pkg, "prerm", `
# $1
#   remove: before remove
#   upgrade: $2(old version), before upgrade
echo "--- prerm"

if [ "$1" = "remove" ]; then
    ${{CmdFile}} stop --family all
else
    :
fi
`)

	AddControlExtraString(pkg, "preinst", `
# $1
#   install: before first install
#   upgrade: $2(old version) $3(new version), before upgrade
echo "--- preinst"

if [ "$1" = "install" ]; then
    :
else
    :
fi
`)

	AddControlExtraString(pkg, "postrm", `
# $1
#   remove: after remove
#   purge: after remove
#   upgrade: $2(old version), after upgrade
echo "--- postrm"

if [ "$1" = "remove" -o "$1" = "purge" ]; then
    rm -f ${{CmdFile}}
    if [ -d /run/systemd/system ]; then
	    systemctl disable ally-wakeup
        rm -f /usr/lib/systemd/system/ally-wakeup.service
        systemctl daemon-reload
    else
        update-rc.d -f ally-wakeup remove
        rm -f /etc/init.d/ally-wakeup
    fi
	if [ "$1" = "purge" ]; then
        rm -rf ${{LogDir}} ${{DataDir}}
	fi
else
    :
fi

`)

	AddControlExtraString(pkg, "postinst", `
# $1
#   configure: after first install
#   configure: $2(new version), after upgrade
echo "--- postinst"

arch="$(lscpu | grep '^Architecture:' | awk '{print $2}')"
[ "$arch" = "i386" -o "$arch" = "i686" ] && arch="i386"
[ "$arch" = "x86_64" ] && arch="amd64"
[ "$arch" = "armv7l" ] && arch="armhf"
[ "$arch" = "aarch64" ] && arch="arm64"
ln -sfT ${{BinFile}}.$arch ${{CmdFile}}

if [ -d /run/systemd/system ]; then
	cat ${{DataDir}}/ally-wakeup.service > /usr/lib/systemd/system/ally-wakeup.service
	systemctl daemon-reload
else
	cat ${{DataDir}}/ally-wakeup > /etc/init.d/ally-wakeup
fi

if [ -z "$2" ]; then
    if [ -d /run/systemd/system ]; then
        systemctl enable ally-wakeup
    else
        cat ${{DataDir}}/ally-wakeup > /etc/init.d/ally-wakeup
        update-rc.d ally-wakeup defaults
    fi
else
    :
fi

`)

	pkg.Write(TargetFile)
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

func AddControlExtra(pkg *debpkg.DebPkg, name, filename string) {
	if err := pkg.AddControlExtra(name, filename); err != nil {
		exit(103, "ERROR: add control extra[%s] with file[%s] to pkg failed, %s\n", name, filename, err)
	}
}
func AddControlExtraString(pkg *debpkg.DebPkg, name, s string) {
	if err := pkg.AddControlExtraString(name, EnvReplace(s)); err != nil {
		exit(103, "ERROR: add controle extra[%s] string to pkg failed, %s\n", name, err)
	}
}
func AddDirectory(pkg *debpkg.DebPkg, dir string) {
	if err := pkg.AddDirectory(dir); err != nil {
		exit(103, "ERROR: add directory[%s] to pkg failed, %s\n", dir, err)
	}
}
func AddEmptyDirectory(pkg *debpkg.DebPkg, dir string) {
	if err := pkg.AddEmptyDirectory(dir); err != nil {
		exit(103, "ERROR: add empty directory[%s] to pkg failed, %s\n", dir, err)
	}
}
func AddFile(pkg *debpkg.DebPkg, filename string, dest ...string) {
	if err := pkg.AddFile(filename, dest...); err != nil {
		exit(103, "ERROR: add file[%s] to pkg failed, %s\n", filename, err)
	}
}
func AddFileString(pkg *debpkg.DebPkg, contents, dest string) {
	if err := pkg.AddFileString(EnvReplace(contents), dest); err != nil {
		exit(103, "ERROR: add file string to pkg failed, %s\n", err)
	}
}

func ReadFile(filename string) string {
	body, err := os.ReadFile(filename)
	if err != nil {
		exit(105, "ERROR: read file[%s] failed, %s\n", filename, err)
	}
	return string(body)
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

func DEBArch(arch string) string {
	switch arch {
	case "386":
		return "i386"
	case "amd64":
		return "amd64"
	case "arm":
		return "armhf"
	case "arm64":
		return "arm64"
	default:
		return "all"
	}
}
