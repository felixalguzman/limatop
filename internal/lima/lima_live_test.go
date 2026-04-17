package lima

import (
	"os/exec"
	"testing"
)

// Live-hits the host's limactl. Skips if limactl is not available.
func TestListLive(t *testing.T) {
	if _, err := exec.LookPath("limactl"); err != nil {
		if _, err2 := exec.LookPath("/home/linuxbrew/.linuxbrew/bin/limactl"); err2 != nil {
			t.Skip("limactl not available")
		}
	}
	vms, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, vm := range vms {
		t.Logf("vm: %-20s status=%-10s arch=%-8s cpus=%d mem=%d disk=%d ssh=%s:%d",
			vm.Name, vm.Status, vm.Arch, vm.CPUs, vm.Memory, vm.Disk, vm.SSHAddress, vm.SSHLocalPort)

		if vm.Status != "Running" {
			continue
		}
		procs, perr := Processes(vm.Name)
		if perr != nil {
			t.Logf("  processes err: %v", perr)
		} else {
			t.Logf("  %d processes; top 3:", len(procs))
			for i := 0; i < 3 && i < len(procs); i++ {
				p := procs[i]
				t.Logf("    pid=%s user=%s cpu=%s mem=%s cmd=%s", p.PID, p.User, p.CPU, p.Mem, p.Cmd)
			}
		}
		u, uerr := GuestUsage(vm.Name)
		if uerr != nil {
			t.Logf("  usage err: %v", uerr)
		} else {
			t.Logf("  usage: cpu=%.1f%% mem=%.1f%% (%d/%d) disk=%.1f%% (%d/%d) up=%s",
				u.CPUPct, u.MemPct, u.MemUsed, u.MemTotal,
				u.DiskPct, u.DiskUsed, u.DiskTotal, u.Uptime)
		}
	}
}
