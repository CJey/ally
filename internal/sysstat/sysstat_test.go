//go:build linux

package sysstat

import (
	"os"
	"testing"
	"time"
)

func TestGetStat(t *testing.T) {
	var pid = os.Getpid()

	stat1, err := GetStat(pid)
	if err != nil {
		t.Fatalf("GetStat failed: %s", err)
	}
	t.Logf("first  call: CPU=%.2f%% Memory=%.1fMB (clkTck=%v pageSize=%v)",
		stat1.CPU, stat1.Memory/1048576, clkTck, pageSize)
	if stat1.Memory < 1<<20 || stat1.Memory > 10<<30 {
		t.Errorf("Memory out of sane range: %f", stat1.Memory)
	}
	if stat1.CPU < 0 {
		t.Errorf("CPU negative: %f", stat1.CPU)
	}

	// 烧 ~200ms CPU 后, 第二次调用的区间 CPU% 应显著大于 0
	var deadline = time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
	}
	stat2, err := GetStat(pid)
	if err != nil {
		t.Fatalf("GetStat failed: %s", err)
	}
	t.Logf("second call: CPU=%.2f%% Memory=%.1fMB", stat2.CPU, stat2.Memory/1048576)
	if stat2.CPU < 30 {
		t.Errorf("expect busy CPU%% > 30 after spin, got %.2f", stat2.CPU)
	}

	// 不存在的进程应报错
	if _, err := GetStat(1 << 30); err == nil {
		t.Errorf("expect error for non-exist pid")
	}
}
