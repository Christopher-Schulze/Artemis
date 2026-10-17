//go:build darwin || linux

package process

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// waitForZombie polls until pid is an unreaped zombie, without reaping it.
func waitForZombie(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
		if err == nil && strings.HasPrefix(strings.TrimSpace(string(out)), "Z") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d never became a zombie", pid)
}

func TestSignalProcessGroupTreatsZombieOnlyGroupAsGone(t *testing.T) {
	command := exec.Command("/bin/sh", "-c", "exit 0")
	ConfigureProcessGroup(command)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waitForZombie(t, command.Process.Pid)
	if err := SignalProcessGroup(command.Process, syscall.SIGKILL); err != nil {
		t.Fatalf("zombie-only owned group reported %v", err)
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := SignalProcessGroup(command.Process, syscall.SIGKILL); err != nil {
		t.Fatalf("reaped owned group reported %v", err)
	}
}

func TestSignalProcessGroupKillsLiveMemberBehindZombieLeader(t *testing.T) {
	command := exec.Command("/bin/sh", "-c", "sleep 30 & echo $!; exit 0")
	ConfigureProcessGroup(command)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 32)
	n, err := stdout.Read(buffer)
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(string(buffer[:n])))
	if err != nil {
		t.Fatal(err)
	}
	waitForZombie(t, command.Process.Pid)
	if err := SignalProcessGroup(command.Process, syscall.SIGKILL); err != nil {
		t.Fatalf("group with a live member reported %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(child, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("live group member %d survived SIGKILL", child)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
}
