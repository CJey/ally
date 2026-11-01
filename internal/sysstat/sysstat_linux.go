package sysstat

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
)

var pageSize = float64(os.Getpagesize())

// clkTck 即 USER_HZ, 内核 ABI 固定为 100 已数十年;
// 仍优先从 auxv 的 AT_CLKTCK 读取, 与 getconf CLK_TCK 等价
var clkTck = func() float64 {
	const AT_CLKTCK = 17
	// auxv 每项是两个 unsigned long，32 位进程(armhf/i386)每项仅 8 字节
	const word = strconv.IntSize / 8
	var load = func(b []byte) uint64 {
		if word == 8 {
			return binary.NativeEndian.Uint64(b)
		}
		return uint64(binary.NativeEndian.Uint32(b))
	}
	if body, err := os.ReadFile("/proc/self/auxv"); err == nil {
		for i := 0; i+2*word <= len(body); i += 2 * word {
			if load(body[i:]) == AT_CLKTCK {
				if v := load(body[i+word:]); v > 0 {
					return float64(v)
				}
			}
		}
	}
	return 100
}()

type record struct {
	utime, stime, uptime float64
}

var mu sync.Mutex
var histories = make(map[int]record)

// getStat 算法与 pidusage.statFromProc 一致:
// CPU% = 两次调用间 (utime+stime) 增量 / 间隔时间; 首次调用间隔时间取进程存活时长
func getStat(pid int) (*SysInfo, error) {
	uptimeBytes, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return nil, err
	}
	uptime, _ := strconv.ParseFloat(strings.SplitN(string(uptimeBytes), " ", 2)[0], 64)

	statBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return nil, err
	}
	// comm 字段含空格/括号, 从最后一个 ')' 之后解析
	raw := string(statBytes)
	idx := strings.LastIndexByte(raw, ')')
	if idx < 0 {
		return nil, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(raw[idx+1:]) // fields[0] 是第3列(state)
	if len(fields) < 22 {
		return nil, fmt.Errorf("malformed /proc/%d/stat, only %d fields", pid, len(fields))
	}
	var f = func(i int) float64 { // i 为 /proc/<pid>/stat 的列号(1起)
		v, _ := strconv.ParseFloat(fields[i-3], 64)
		return v
	}
	var utime, stime = f(14), f(15)
	var start = f(22) / clkTck
	var rss = f(24)

	mu.Lock()
	defer mu.Unlock()
	var last = histories[pid]

	var total = (utime - last.utime + stime - last.stime) / clkTck
	var seconds = start - uptime
	if last.uptime != 0 {
		seconds = uptime - last.uptime
	}
	seconds = math.Abs(seconds)
	if seconds == 0 {
		seconds = 1
	}
	histories[pid] = record{utime: utime, stime: stime, uptime: uptime}

	return &SysInfo{
		CPU:    total / seconds * 100,
		Memory: rss * pageSize,
	}, nil
}
