// Package sysinfo 采集服务器基础运维信息（CPU/内存/磁盘/负载/uptime/端口/登录）。
// 全部用 Go 标准库 + 读 /proc 实现，零外部依赖，保持单二进制轻量。
package sysinfo

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Info 服务器基础信息
type Info struct {
	Hostname       string    `json:"hostname"`        // 主机名
	OS             string    `json:"os"`              // 操作系统
	Arch           string    `json:"arch"`            // 架构
	CPU            CPUInfo   `json:"cpu"`             // CPU 信息
	Memory         MemInfo   `json:"memory"`          // 内存信息
	Disk           DiskInfo  `json:"disk"`            // 磁盘信息
	Uptime         string    `json:"uptime"`          // 运行时长(人类可读)
	UptimeSec      int64     `json:"uptime_sec"`      // 运行秒数
	LoadAvg        string    `json:"load_avg"`        // 系统负载(1/5/15分钟)
	OpenPorts      []string  `json:"open_ports"`      // 监听端口列表
	TCPConnections int       `json:"tcp_connections"` // TCP 连接数
	LastLogins     []string  `json:"last_logins"`     // 最近登录(SSH)
	Time           time.Time `json:"time"`            // 采集时间
}

// CPUInfo CPU 信息
type CPUInfo struct {
	Cores int     `json:"cores"` // 逻辑核心数
	Usage float64 `json:"usage"` // CPU 使用率(%)
}

// MemInfo 内存信息
type MemInfo struct {
	TotalGB float64 `json:"total_gb"` // 总内存(GB)
	UsedGB  float64 `json:"used_gb"`  // 已用内存(GB)
	UsedPct float64 `json:"used_pct"` // 使用率(%)
}

// DiskInfo 磁盘信息
type DiskInfo struct {
	TotalGB float64 `json:"total_gb"` // 总磁盘(GB)
	UsedGB  float64 `json:"used_gb"`  // 已用磁盘(GB)
	UsedPct float64 `json:"used_pct"` // 使用率(%)
}

// Collect 采集服务器基础信息。
func Collect() Info {
	info := Info{
		Hostname: hostname(),
		OS:       osName(),
		Arch:     runtime.GOARCH,
		CPU:      cpuInfo(),
		Memory:   memInfo(),
		Disk:     diskInfo(),
		Uptime:   uptime(),
		LoadAvg:  loadAvg(),
		Time:     time.Now(),
	}
	info.UptimeSec = uptimeSec()
	info.OpenPorts = openPorts()
	info.TCPConnections = tcpConnections()
	info.LastLogins = lastLogins()
	return info
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

func osName() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
		}
	}
	return runtime.GOOS
}

// cpuInfo 从 /proc/stat 读取 CPU 使用率。
// 算法：两次采样间隔读取 idle/total 差值，usage = (total - idle) / total。
func cpuInfo() CPUInfo {
	cores := runtime.NumCPU()
	return CPUInfo{Cores: cores, Usage: cpuUsage()}
}

func readCPUStat() (idle, total uint64) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0
	}
	line := strings.Fields(strings.Split(string(data), "\n")[0]) // "cpu  user nice system idle iowait irq softirq"
	if len(line) < 8 {
		return 0, 0
	}
	var vals []uint64
	for _, s := range line[1:] {
		v, _ := strconv.ParseUint(s, 10, 64)
		vals = append(vals, v)
	}
	idle = vals[3] // idle
	for _, v := range vals {
		total += v
	}
	return idle, total
}

func cpuUsage() float64 {
	idle1, total1 := readCPUStat()
	time.Sleep(200 * time.Millisecond)
	idle2, total2 := readCPUStat()
	if total2 <= total1 {
		return 0
	}
	idleDelta := idle2 - idle1
	totalDelta := total2 - total1
	return float64(totalDelta-idleDelta) / float64(totalDelta) * 100
}

// memInfo 从 /proc/meminfo 读取内存。
func memInfo() MemInfo {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return MemInfo{}
	}
	kv := map[string]uint64{}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			key := strings.TrimSuffix(parts[0], ":")
			v, _ := strconv.ParseUint(parts[1], 10, 64)
			kv[key] = v
		}
	}
	total := kv["MemTotal"] // kB
	avail := kv["MemAvailable"]
	if avail == 0 {
		avail = kv["MemFree"]
	}
	used := total - avail
	return MemInfo{
		TotalGB: float64(total) / 1024 / 1024,
		UsedGB:  float64(used) / 1024 / 1024,
		UsedPct: pct(used, total),
	}
}

// diskInfo 从 /proc/mounts 找根分区，用 statfs 计算使用率。
func diskInfo() DiskInfo {
	// 优先用 df 命令（最可靠），失败则读 /proc
	if out, err := exec.Command("df", "-k", "/").Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		if len(lines) >= 2 {
			fields := strings.Fields(lines[1])
			if len(fields) >= 5 {
				total, _ := strconv.ParseUint(fields[1], 10, 64)
				used, _ := strconv.ParseUint(fields[2], 10, 64)
				return DiskInfo{
					TotalGB: float64(total) / 1024 / 1024,
					UsedGB:  float64(used) / 1024 / 1024,
					UsedPct: pct(used, total),
				}
			}
		}
	}
	return DiskInfo{}
}

func pct(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}

// uptimeSec 从 /proc/uptime 读取运行秒数。
func uptimeSec() int64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0
	}
	sec, _ := strconv.ParseFloat(fields[0], 64)
	return int64(sec)
}

func uptime() string {
	sec := uptimeSec()
	d := sec / 86400
	h := (sec % 86400) / 3600
	m := (sec % 3600) / 60
	if d > 0 {
		return formatDuration(d, h, m)
	}
	if h > 0 {
		return formatDuration(0, h, m)
	}
	return formatDuration(0, 0, m)
}

func formatDuration(d, h, m int64) string {
	parts := []string{}
	if d > 0 {
		parts = append(parts, strconv.FormatInt(d, 10)+"天")
	}
	if h > 0 {
		parts = append(parts, strconv.FormatInt(h, 10)+"时")
	}
	parts = append(parts, strconv.FormatInt(m, 10)+"分")
	return strings.Join(parts, "")
}

func loadAvg() string {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return ""
	}
	return fields[0] + " " + fields[1] + " " + fields[2]
}

// openPorts 用 ss 命令列出监听端口。
func openPorts() []string {
	out, err := exec.Command("ss", "-tlnp").Output()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var ports []string
	for _, line := range strings.Split(string(out), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		local := fields[3] // 如 0.0.0.0:443 或 *:22
		idx := strings.LastIndex(local, ":")
		if idx < 0 {
			continue
		}
		port := local[idx+1:]
		if port == "" || seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
	}
	return ports
}

// tcpConnections 统计当前 TCP 连接数（读 /proc/net/tcp + tcp6，行数-1去掉表头）。
func tcpConnections() int {
	count := 0
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		if len(lines) > 1 {
			count += len(lines) - 1
		}
	}
	return count
}

// lastLogins 用 last 命令获取最近 SSH 登录。
func lastLogins() []string {
	out, err := exec.Command("last", "-n", "5").Output()
	if err != nil {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "wtmp") {
			continue
		}
		lines = append(lines, line)
		if len(lines) >= 5 {
			break
		}
	}
	return lines
}
