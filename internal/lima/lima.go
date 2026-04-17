package lima

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type VM struct {
	Name         string `json:"name"`
	Hostname     string `json:"hostname"`
	Status       string `json:"status"`
	Dir          string `json:"dir"`
	VMType       string `json:"vmType"`
	Arch         string `json:"arch"`
	CPUs         int    `json:"cpus"`
	Memory       int64  `json:"memory"`
	Disk         int64  `json:"disk"`
	SSHLocalPort int    `json:"sshLocalPort"`
	SSHAddress   string `json:"sshAddress"`
	LimaVersion  string `json:"limaVersion"`
	HostOS       string `json:"HostOS"`
	HostArch     string `json:"HostArch"`
	Config       struct {
		OS     string `json:"os"`
		Images []struct {
			Location string `json:"location"`
			Arch     string `json:"arch"`
		} `json:"images"`
	} `json:"config"`
}

type Process struct {
	PID  string
	User string
	CPU  string
	Mem  string
	Cmd  string
}

// Usage summarizes a guest's live resource usage (percentages 0..100).
type Usage struct {
	CPUPct    float64
	MemPct    float64
	MemUsed   int64
	MemTotal  int64
	DiskPct   float64
	DiskUsed  int64
	DiskTotal int64
	Uptime    string
}

func bin() string {
	if p, err := exec.LookPath("limactl"); err == nil {
		return p
	}
	for _, p := range []string{
		"/home/linuxbrew/.linuxbrew/bin/limactl",
		"/opt/homebrew/bin/limactl",
		"/usr/local/bin/limactl",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "limactl"
}

func runOut(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin(), args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%s: %s", err, msg)
	}
	return out.Bytes(), nil
}

// List returns all configured Lima VMs.
func List() ([]VM, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	out, err := runOut(ctx, "list", "--json")
	if err != nil {
		return nil, err
	}
	var vms []VM
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var v VM
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		vms = append(vms, v)
	}
	return vms, nil
}

// Start boots the named VM. Blocks until `limactl start` exits.
func Start(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	_, err := runOut(ctx, "start", name)
	return err
}

// Stop shuts the named VM down gracefully.
func Stop(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, err := runOut(ctx, "stop", name)
	return err
}

// ShellCmd returns an *exec.Cmd that opens an interactive guest shell.
// The caller is responsible for wiring up stdio (typically via tea.ExecProcess).
func ShellCmd(name string) *exec.Cmd {
	return exec.Command(bin(), "shell", name)
}

// Processes runs `ps` inside the guest and returns the top processes sorted by CPU.
func Processes(name string) ([]Process, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	out, err := runOut(ctx, "shell", name, "ps", "-eo", "pid,user,pcpu,pmem,comm", "--sort=-pcpu", "--no-headers")
	if err != nil {
		return nil, err
	}
	var procs []Process
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		procs = append(procs, Process{
			PID:  fields[0],
			User: fields[1],
			CPU:  fields[2],
			Mem:  fields[3],
			Cmd:  strings.Join(fields[4:], " "),
		})
	}
	return procs, nil
}

// GuestUsage returns live CPU/mem/disk usage from inside the guest.
// It runs a single shell invocation that prints three space-separated values per line.
func GuestUsage(name string) (Usage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	script := `awk '/^cpu / {t=$2+$3+$4+$5+$6+$7+$8; i=$5+$6; u=t-i; print "cpu", u, t}' /proc/stat; ` +
		`awk '/MemTotal:/ {t=$2} /MemAvailable:/ {a=$2} END {print "mem", (t-a)*1024, t*1024}' /proc/meminfo; ` +
		`df -B1 --output=used,size / | tail -1 | awk '{print "disk", $1, $2}'; ` +
		`awk '{print "up", int($1)}' /proc/uptime`
	out, err := runOut(ctx, "shell", name, "sh", "-c", script)
	if err != nil {
		return Usage{}, err
	}
	u := Usage{}
	var cpuUsed1, cpuTotal1 int64
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "cpu":
			if len(fields) >= 3 {
				fmt.Sscan(fields[1], &cpuUsed1)
				fmt.Sscan(fields[2], &cpuTotal1)
			}
		case "mem":
			if len(fields) >= 3 {
				fmt.Sscan(fields[1], &u.MemUsed)
				fmt.Sscan(fields[2], &u.MemTotal)
			}
		case "disk":
			if len(fields) >= 3 {
				fmt.Sscan(fields[1], &u.DiskUsed)
				fmt.Sscan(fields[2], &u.DiskTotal)
			}
		case "up":
			if len(fields) >= 2 {
				var secs int64
				fmt.Sscan(fields[1], &secs)
				u.Uptime = humanDuration(secs)
			}
		}
	}
	if u.MemTotal > 0 {
		u.MemPct = 100 * float64(u.MemUsed) / float64(u.MemTotal)
	}
	if u.DiskTotal > 0 {
		u.DiskPct = 100 * float64(u.DiskUsed) / float64(u.DiskTotal)
	}
	// Snapshot CPU twice with a small gap to get a delta.
	time.Sleep(400 * time.Millisecond)
	out2, err := runOut(ctx, "shell", name, "sh", "-c",
		`awk '/^cpu / {t=$2+$3+$4+$5+$6+$7+$8; i=$5+$6; u=t-i; print u, t}' /proc/stat`)
	if err == nil {
		var cpuUsed2, cpuTotal2 int64
		fmt.Sscan(strings.TrimSpace(string(out2)), &cpuUsed2, &cpuTotal2)
		dt := cpuTotal2 - cpuTotal1
		du := cpuUsed2 - cpuUsed1
		if dt > 0 {
			u.CPUPct = 100 * float64(du) / float64(dt)
		}
	}
	return u, nil
}

func humanDuration(secs int64) string {
	d := time.Duration(secs) * time.Second
	days := int64(d.Hours()) / 24
	hours := int64(d.Hours()) % 24
	mins := int64(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}
