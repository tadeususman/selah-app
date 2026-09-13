package handlers

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
)

type serverStatusData struct {
	Uptime      string
	LoadAvg     string
	MemTotal    uint64
	MemUsed     uint64
	MemPercent  int
	DiskTotal   uint64
	DiskUsed    uint64
	DiskPercent int
}

func (a *App) AdminServerStatus(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var d serverStatusData

	// Uptime
	if raw, err := os.ReadFile("/proc/uptime"); err == nil {
		fields := strings.Fields(string(raw))
		if len(fields) > 0 {
			if secs, err := strconv.ParseFloat(fields[0], 64); err == nil {
				d.Uptime = formatUptime(int(secs))
			}
		}
	}

	// Load average
	if raw, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(raw))
		if len(fields) >= 3 {
			d.LoadAvg = fields[0] + " " + fields[1] + " " + fields[2]
		}
	}

	// Memory
	if f, err := os.Open("/proc/meminfo"); err == nil {
		defer f.Close()
		mem := map[string]uint64{}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			parts := strings.Fields(scanner.Text())
			if len(parts) >= 2 {
				val, _ := strconv.ParseUint(parts[1], 10, 64)
				mem[strings.TrimSuffix(parts[0], ":")] = val * 1024 // kB → bytes
			}
		}
		total := mem["MemTotal"]
		avail := mem["MemAvailable"]
		if total > 0 {
			used := total - avail
			d.MemTotal = total
			d.MemUsed = used
			d.MemPercent = int(float64(used) / float64(total) * 100)
		}
	}

	// Disk (root)
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil {
		total := stat.Blocks * uint64(stat.Bsize)
		free := stat.Bavail * uint64(stat.Bsize)
		used := total - free
		d.DiskTotal = total
		d.DiskUsed = used
		if total > 0 {
			d.DiskPercent = int(float64(used) / float64(total) * 100)
		}
	}

	a.render(w, "admin_server.html", d)
}

func formatUptime(secs int) string {
	days := secs / 86400
	secs %= 86400
	hours := secs / 3600
	secs %= 3600
	mins := secs / 60
	if days > 0 {
		return fmt.Sprintf("%d hari %d jam %d menit", days, hours, mins)
	}
	if hours > 0 {
		return fmt.Sprintf("%d jam %d menit", hours, mins)
	}
	return fmt.Sprintf("%d menit", mins)
}

func fmtBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
