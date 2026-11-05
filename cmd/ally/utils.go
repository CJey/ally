package main

import (
	"cmp"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
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
		return "", fmt.Errorf("get the program fullpath failed, %w", err)
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
	slices.SortFunc(addrs, func(a, b *pb.Address) int {
		return cmp.Or(
			cmp.Compare(a.Created, b.Created),
			cmp.Compare(b.Updated, a.Updated),
			cmp.Compare(b.Refs, a.Refs),
			cmp.Compare(a.Address, b.Address),
		)
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
