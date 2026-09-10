//go:build !linux

package tools

// sysmon reads Linux-specific sources (/proc, cgroupfs, Statfs) that only
// exist on the linux GOOS; the deploy target is exclusively linux/arm64
// (S32). Other platforms (used for local dev/CI builds) get a clear
// "unsupported" result instead of a compile failure.

const sysmonUnsupportedMsg = "sysmon: this action is only supported on Linux (the deployment target); " +
	"not available on this build's OS"

func sysmonMemResult() *ToolResult {
	return ErrorResult(sysmonUnsupportedMsg)
}

func sysmonTopResult(int) *ToolResult {
	return ErrorResult(sysmonUnsupportedMsg)
}

func sysmonLoadResult() *ToolResult {
	return ErrorResult(sysmonUnsupportedMsg)
}

func sysmonDiskResult() *ToolResult {
	return ErrorResult(sysmonUnsupportedMsg)
}

func sysmonKillResult(int) *ToolResult {
	return ErrorResult(sysmonUnsupportedMsg)
}

func sysmonReniceResult(int, int) *ToolResult {
	return ErrorResult(sysmonUnsupportedMsg)
}
