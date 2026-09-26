// Package gate is the list. Anything that would lock the house out is
// refused here in code, however it is asked for — from the panel, from a
// window, from an outside assistant, from a task. The structural half of
// the gate (every operation names a host, and the hub is not one) lives
// in the store's host resolver; this is the other half.
//
// The same list, plus the hub's hold and the fleet's addresses, ships to
// every remote as an omp hook, so a remote's root agent meets it too.
package gate

import (
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Refusal is what a refused command comes back as: an error the caller
// must relay, with the one reason.
type Refusal struct{ Reason string }

func (r *Refusal) Error() string { return "refused: " + r.Reason }

// IsRefusal reports whether err is the gate speaking.
func IsRefusal(err error) bool {
	var r *Refusal
	return errors.As(err, &r)
}

// A rule looks at one simple command — program and arguments, sudo and
// environment assignments stripped — and says why it is refused, or "".
type rule func(prog string, args []string) string

// AgentAccount owns the agent's state on a remote (its home, AgentHome,
// holds omp's config, credentials and sessions). A job runs as root with
// that home; the account itself does nothing.
const (
	AgentAccount = "homedash-agent"
	AgentHome    = "/home/" + AgentAccount
)

// systemPaths are what a recursive delete may never name.
var systemPaths = map[string]bool{
	"/": true, "/*": true, "/etc": true, "/etc/*": true, "/usr": true, "/usr/*": true,
	"/var": true, "/var/*": true, "/boot": true, "/lib": true, "/lib64": true, "/bin": true,
	"/sbin": true, "/home": true, "/home/*": true, "/root": true, "/dev": true, "/proc": true,
	"/sys": true, "/opt": true, "/srv": true,
}

var rules = []rule{
	// switching off a firewall
	func(p string, a []string) string {
		switch {
		case p == "ufw" && len(a) > 0 && (a[0] == "disable" || a[0] == "reset"),
			p == "iptables" && has(a, "-F", "--flush"),
			p == "nft" && strings.Join(a, " ") == "flush ruleset",
			p == "systemctl" && len(a) > 1 && has(a[:1], "stop", "disable", "mask") && has(a[1:], "ufw", "firewalld", "nftables"):
			return "switching off the firewall"
		}
		return ""
	},
	// cutting the hub's own SSH access
	func(p string, a []string) string {
		switch {
		case p == "systemctl" && len(a) > 1 && has(a[:1], "stop", "disable", "mask") && has(a[1:], "ssh", "sshd", "ssh.socket", "ssh.service", "sshd.service"),
			has([]string{p}, "rm", "mv", "truncate", "shred") && anyPath(a, "/etc/ssh/", "authorized_keys"),
			has([]string{p}, "deluser", "userdel") && has(a, "homedash", AgentAccount),
			p == "passwd" && has(a, "-l") && has(a, "homedash", AgentAccount),
			p == "usermod" && has(a, "-L") && has(a, "homedash", AgentAccount):
			return "cutting the hub's own SSH access"
		}
		return ""
	},
	// editing SSH config by hand
	func(p string, a []string) string {
		if has([]string{p}, "sed", "perl", "ed", "python", "python3", "tee") && anyPath(a, "/etc/ssh/") {
			return "editing SSH config by hand"
		}
		return ""
	},
	// deleting a system path
	func(p string, a []string) string {
		if p != "rm" {
			return ""
		}
		recursive := false
		for _, x := range a {
			if x == "--recursive" || (strings.HasPrefix(x, "-") && !strings.HasPrefix(x, "--") && strings.ContainsAny(x, "rR")) {
				recursive = true
			}
		}
		if !recursive {
			return ""
		}
		for _, x := range a {
			if systemPaths[strings.TrimSuffix(x, "/")] || systemPaths[x] {
				return "deleting a system path"
			}
		}
		return ""
	},
	// shutting down or rebooting a machine
	func(p string, a []string) string {
		switch {
		case has([]string{p}, "shutdown", "poweroff", "halt", "reboot"),
			p == "init" && has(a, "0", "6"),
			p == "systemctl" && has(a, "poweroff", "halt", "reboot", "kexec"):
			return "shutting down or rebooting a machine"
		}
		return ""
	},
}

// diskTools write a disk's layout or contents. Each is refused when it
// names a system device (see systemDevice); every other disk is a data
// disk and theirs to format.
var diskTools = map[string]bool{
	"fdisk": true, "sfdisk": true, "parted": true, "gdisk": true, "sgdisk": true, "cfdisk": true,
	"wipefs": true, "mkswap": true, "dd": true, "tune2fs": true, "resize2fs": true, "mdadm": true,
	"pvcreate": true, "vgcreate": true, "lvcreate": true, "pvremove": true, "vgremove": true,
	"lvremove": true, "cryptsetup": true,
}

const systemDiskReason = "repartitioning or wiping the system disk"

// pseudoDevices are under /dev but are not disks.
var pseudoDevices = map[string]bool{"/dev/null": true, "/dev/zero": true, "/dev/random": true, "/dev/urandom": true, "/dev/stdin": true, "/dev/stdout": true, "/dev/stderr": true, "/dev/tty": true}

// systemDevice says whether a word names a device the machine runs from,
// or one the gate cannot tell apart from it. system is the machine's
// systemDevices from its facts — every source /, /boot, /var, /home and
// swap sit on and the whole chain beneath each — compared by kernel
// name, so /dev/mapper/vg-root meets "vg-root" and /dev/sda1 meets
// "sda". nil means the machine has not said, and every disk is refused.
func systemDevice(word string, system []string) string {
	w := strings.Trim(word, `"'`)
	if strings.HasPrefix(w, "if=") {
		return "" // dd's input is read, not written
	}
	if i := strings.LastIndex(w, "="); i >= 0 && i+1 < len(w) && w[i+1] == '/' {
		w = w[i+1:]
	}
	if !strings.HasPrefix(w, "/dev/") || pseudoDevices[w] || strings.HasPrefix(w, "/dev/fd/") {
		return ""
	}
	if system == nil {
		return systemDiskReason + " (this remote has not reported which disk carries its system)"
	}
	name := strings.TrimPrefix(w, "/dev/mapper/")
	name = strings.TrimPrefix(name, "/dev/")
	if strings.Contains(name, "/") {
		return systemDiskReason + " (name the device by its kernel name, not " + w + ")"
	}
	for _, s := range system {
		s = s[strings.LastIndex(s, "/")+1:]
		if name == s {
			return systemDiskReason
		}
		if rest, ok := strings.CutPrefix(name, s); ok {
			rest = strings.TrimPrefix(rest, "p")
			if rest != "" && strings.Trim(rest, "0123456789") == "" {
				return systemDiskReason
			}
		}
	}
	return ""
}

var (
	segment   = regexp.MustCompile(`\|\||&&|[;|\n]`)
	redirect  = regexp.MustCompile(`>>?\s*(/etc/ssh/\S*)`)
	devWrite  = regexp.MustCompile(`>>?\s*(/dev/\S+)`)
	assignRe  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	wrappers  = map[string]bool{"sudo": true, "doas": true, "nohup": true, "env": true, "command": true, "exec": true, "time": true, "nice": true}
	forkBombs = regexp.MustCompile(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`)
)

// Check refuses a command line the list matches, with no machine in
// hand: every disk counts as the system disk. It is deliberately
// textual: the point is that the refusal is the same whoever is asking.
func Check(command string) error { return CheckOn(command, nil) }

// CheckOn is Check for a known machine: system is its systemDevices, and
// a disk tool is refused only against one of those.
func CheckOn(command string, system []string) error {
	if forkBombs.MatchString(command) {
		return &Refusal{Reason: "a fork bomb: " + short(command)}
	}
	if m := redirect.FindStringSubmatch(command); m != nil {
		return &Refusal{Reason: "editing SSH config by hand: " + short(command)}
	}
	for _, m := range devWrite.FindAllStringSubmatch(command, -1) {
		if reason := systemDevice(m[1], system); reason != "" {
			return &Refusal{Reason: reason + ": " + short(command)}
		}
	}
	for _, seg := range segment.Split(command, -1) {
		words := strings.Fields(seg)
		for len(words) > 0 && (wrappers[words[0]] || assignRe.MatchString(words[0]) || strings.HasPrefix(words[0], "-")) {
			words = words[1:]
		}
		if len(words) == 0 {
			continue
		}
		prog := words[0]
		if i := strings.LastIndex(prog, "/"); i >= 0 {
			prog = prog[i+1:]
		}
		for _, r := range rules {
			if reason := r(prog, words[1:]); reason != "" {
				return &Refusal{Reason: reason + ": " + short(command)}
			}
		}
		if diskTools[prog] || strings.HasPrefix(prog, "mkfs") {
			for _, x := range words[1:] {
				if reason := systemDevice(x, system); reason != "" {
					return &Refusal{Reason: reason + ": " + short(command)}
				}
			}
		}
	}
	return nil
}

func has(xs []string, any ...string) bool {
	for _, x := range xs {
		for _, a := range any {
			if x == a {
				return true
			}
		}
	}
	return false
}

func anyPath(xs []string, subs ...string) bool {
	for _, x := range xs {
		for _, s := range subs {
			if strings.Contains(x, s) {
				return true
			}
		}
	}
	return false
}

func short(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 80 {
		s = s[:77] + "..."
	}
	return fmt.Sprintf("%q", s)
}

// Reasons lists what the gate refuses, for the panel and the hook.
func Reasons() []string {
	return []string{
		"switching off the firewall",
		"cutting the hub's own SSH access",
		"editing SSH config by hand",
		systemDiskReason,
		"deleting a system path",
		"shutting down or rebooting a machine",
	}
}

// Hook is the same list as an omp hook, written to every remote by
// enrollment and again before every job round.
//
//go:embed hook.ts
var Hook string

// --- the compose gate ---------------------------------------------------

// The compose gate: a docker danger list read from a compose file's YAML,
// for a stack an agent deploys from the hub.
var (
	composePrivileged = regexp.MustCompile(`(?im)^\s*privileged\s*:\s*["']?true["']?\s*$`)
	composePIDHost    = regexp.MustCompile(`(?im)^\s*pid\s*:\s*["']?host["']?\s*$`)
	composeNetHost    = regexp.MustCompile(`(?im)^\s*network_mode\s*:\s*["']?host["']?\s*$`)
	composeCapAdd     = regexp.MustCompile(`(?im)^\s*cap_add\s*:\s*(.*)$`)
	composeCapItem    = regexp.MustCompile(`(?i)^-?\s*["']?(SYS_ADMIN|ALL)["']?\s*$`)
	composeBindItem   = regexp.MustCompile(`(?im)^\s*-\s*["']?(/var/run/docker\.sock|/)["']?\s*:`)
	composeBindSource = regexp.MustCompile(`(?im)^\s*source\s*:\s*["']?(/var/run/docker\.sock|/)["']?\s*$`)
)

// ComposeRefusal is the compose-content half of the same list: what an
// agent's deploy_stack may not bring up, because nothing else inspects a
// compose file's content before it lands on a remote and comes up as a
// container. "" when the compose is fine. Deliberately textual, like
// Check: a compose file is YAML, but line-by-line
// patterns are enough for the handful of stanzas that matter and don't
// need a parser to keep in sync with docker compose's own schema.
func ComposeRefusal(compose string) string {
	switch {
	case composePrivileged.MatchString(compose):
		return "a privileged container (privileged: true)"
	case composePIDHost.MatchString(compose):
		return "sharing the host's PID namespace (pid: host)"
	case composeNetHost.MatchString(compose):
		return "sharing the host's network (network_mode: host)"
	}
	if m := composeCapAdd.FindStringSubmatch(compose); m != nil {
		if reason := capAddReason(compose, m); reason != "" {
			return reason
		}
	}
	if composeBindItem.MatchString(compose) || composeBindSource.MatchString(compose) {
		return "a bind mount of / or the docker socket"
	}
	return ""
}

// capAddReason looks at a cap_add stanza, inline (`cap_add: [SYS_ADMIN]`)
// or as a following list, for SYS_ADMIN or ALL.
func capAddReason(compose string, m []string) string {
	inline := strings.TrimSpace(m[1])
	if inline != "" && inline != "[]" {
		if composeCapItem.MatchString(strings.Trim(inline, "[]")) || strings.Contains(strings.ToUpper(inline), "SYS_ADMIN") || strings.Contains(strings.ToUpper(inline), "ALL") {
			return "adding a dangerous capability (cap_add)"
		}
		return ""
	}
	// A block list: every following, more-indented "- ITEM" line until
	// one that isn't.
	lines := strings.Split(compose, "\n")
	at := 0
	for i, l := range lines {
		if composeCapAdd.MatchString(l) {
			at = i
			break
		}
	}
	indent := leadingSpace(lines[at])
	for _, l := range lines[at+1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if leadingSpace(l) <= indent {
			break
		}
		if composeCapItem.MatchString(strings.TrimSpace(l)) {
			return "adding a dangerous capability (cap_add)"
		}
	}
	return ""
}

func leadingSpace(s string) int {
	return len(s) - len(strings.TrimLeft(s, " \t"))
}
