package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"

	pb "github.com/cjey/ally/proto"
)

func Exit(code int, format string, a ...any) {
	L.Printf(format, a...)
	if l := log.Default(); L != l {
		l.Printf(format, a...)
	}
	os.Exit(code)
}

func LookPath(bin string) (string, error) {
	if val, err := exec.LookPath(bin); err != nil {
		return "", fmt.Errorf("lookup the program path failed, %w", err)
	} else if val, err := filepath.Abs(val); err != nil {
		return "", fmt.Errorf("get the proram fullpath failed, %w", err)
	} else {
		bin = val
	}

	if stat, err := os.Stat(bin); err != nil {
		return "", fmt.Errorf("stat the program file failed, %w", err)
	} else if mode := stat.Mode(); !mode.IsRegular() || (mode&0111 == 0) {
		return "", fmt.Errorf("the program file is not a regular file or not executable")
	}

	return bin, nil
}

func SortAddress(addrs []*pb.Address) {
	sort.Slice(addrs, func(i, j int) bool {
		if addrs[i].Created < addrs[j].Created {
			return true
		}
		if addrs[i].Updated > addrs[j].Updated {
			return true
		}
		if addrs[i].Refs > addrs[j].Refs {
			return true
		}
		return addrs[i].Address < addrs[j].Address
	})
}

func SortInstanceInfo(insts []*pb.InstanceInfo) {
	sort.Slice(insts, func(i, j int) bool {
		return insts[i].Id > insts[j].Id
	})
}

var _RegGoodName = regexp.MustCompile(`^[a-zA-Z]([0-9a-zA-Z.\-_]*[0-9a-zA-Z])?$`)

func IsGoodName(name string) bool {
	return _RegGoodName.MatchString(name)
}
