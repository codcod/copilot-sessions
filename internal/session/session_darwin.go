package session

import (
	"bytes"
	"encoding/binary"
	"slices"

	"golang.org/x/sys/unix"
)

// pidIsCopilot reports whether any argv string of pid contains "copilot". Any sysctl error
// (EPERM on another user's process, a gone PID) reads as false.
func pidIsCopilot(pid int) bool {
	b, err := unix.SysctlRaw("kern.procargs2", pid)
	return err == nil && argvHasCopilot(b)
}

// argvHasCopilot reports whether an argv string of a kern.procargs2 buffer contains
// "copilot". The environment after argv is not matched: COPILOT_HOME would flag anything.
func argvHasCopilot(b []byte) bool {
	return slices.ContainsFunc(procargsArgv(b), func(a []byte) bool {
		return bytes.Contains(a, []byte("copilot"))
	})
}

// procargsArgv parses a kern.procargs2 buffer: argc (native int32), the exec path, NUL
// padding, then argc NUL-terminated argv strings (then the environment, ignored). A short or
// malformed buffer returns nil. ponytail: an empty argv[0] is indistinguishable from the
// padding (ps has the same blind spot), so the window then shifts one string into the env;
// the error is a false "ongoing", the safe direction.
func procargsArgv(b []byte) [][]byte {
	if len(b) < 4 {
		return nil
	}
	argc := int(int32(binary.NativeEndian.Uint32(b)))
	if argc <= 0 {
		return nil
	}
	i := bytes.IndexByte(b[4:], 0)
	if i < 0 {
		return nil
	}
	rest := bytes.TrimLeft(b[4+i:], "\x00")
	argv := make([][]byte, 0, min(argc, len(b)))
	for range argc {
		i := bytes.IndexByte(rest, 0)
		if i < 0 {
			return nil
		}
		argv = append(argv, rest[:i])
		rest = rest[i+1:]
	}
	return argv
}

func ancestorsFrom(pid int) map[int]bool {
	pids := map[int]bool{}
	for pid > 1 {
		pids[pid] = true
		k, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
		if err != nil {
			break
		}
		ppid := int(k.Eproc.Ppid)
		if ppid == pid {
			break
		}
		pid = ppid
	}
	return pids
}
