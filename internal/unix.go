package internal

import (
	"fmt"
	"strconv"
	"strings"
)

// UCred2Addr 将pid和uid做简单的一次编码，融合为一个字符串
func UCred2Addr(pid, uid int) string {
	return fmt.Sprintf("ally:%d:%d", pid, uid)
}

// Addr2UCred 从字符串地址中解码出UCred2Addr编码的pid和uid
func Addr2UCred(str string) (pid, uid int) {
	fields := strings.Split(str, ":")
	if len(fields) != 3 || fields[0] != "ally" {
		return
	}
	v1, e1 := strconv.Atoi(fields[1])
	v2, e2 := strconv.Atoi(fields[2])
	if e1 != nil || e2 != nil || v1 == 0 {
		return
	}
	return v1, v2
}
