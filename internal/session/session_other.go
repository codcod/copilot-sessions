//go:build !linux

package session

// Without a process table to read, no lock PID counts as Copilot (locked sessions read as
// stale) and the ancestry is only the PID itself.

func pidIsCopilot(int) bool { return false }

func ancestorsFrom(pid int) map[int]bool { return map[int]bool{pid: true} }
