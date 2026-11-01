// Package sysstat 提供进程CPU/内存占用查询, 是 github.com/struCoder/pidusage 的纯Go替代。
// pidusage 的包级 init 会 fork+exec 两次 getconf(CLK_TCK/PAGESIZE),
// 导致所有链接了它的二进制每次启动都多付 ~3ms 和两个子进程,
// 且依赖运行环境中存在 getconf 可执行文件(精简容器中常缺失)。
// 本包读 /proc 与 auxv 实现同等语义, 零 fork、零外部依赖。
package sysstat

// SysInfo 与 pidusage.SysInfo 字段语义一致
type SysInfo struct {
	CPU    float64 // CPU占用百分比: 首次调用为进程生命周期均值, 后续调用为距上次调用期间的均值
	Memory float64 // 常驻内存RSS, 单位字节
}

func GetStat(pid int) (*SysInfo, error) {
	return getStat(pid)
}
