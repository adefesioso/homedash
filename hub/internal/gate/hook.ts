// HomeDash's refused-command list, as an omp hook. The hub writes it,
// root-owned, to the agent's ~/.omp/agent/hooks/pre/ at enrollment and
// again before every job round, so a job cannot disarm the next one. A
// job runs as root: this hook is the stated rule and the log line, not
// the control (see docs/running/safety.md). The logic mirrors
// internal/gate/gate.go's Check, plus three things a root job may not do:
// touch the hub's hold on this machine (HOLD, for every tool), and name
// the hub or another remote (FLEET, the addresses the hub writes to
// /etc/homedash/fleet-addrs before the round).
const HOLD = ["/etc/ssh/", "authorized_keys", "/etc/sudoers", "/etc/pam.d/", "/etc/homedash/", "/home/homedash/",
  "/usr/local/bin/homedash-", "/usr/local/bin/omp", "/home/homedash-agent/.omp/agent/hooks/"];
const holdPath = (word: string): boolean => {
  let w = String(word ?? "").replace(/^["']|["']$/g, "");
  const eq = w.lastIndexOf("=");
  if (eq >= 0 && w[eq + 1] === "/") w = w.slice(eq + 1);
  return HOLD.some((p) => p.endsWith("/") ? (w === p.slice(0, -1) || w.startsWith(p))
    : !p.startsWith("/") ? w.includes(p) : w.startsWith(p));
};
let FLEET: string[] = [];
try {
  const { readFileSync } = require("node:fs");
  FLEET = String(readFileSync("/etc/homedash/fleet-addrs", "utf8")).split("\n").map((x) => x.trim()).filter(Boolean);
} catch { FLEET = []; }
const namesFleet = (command: string): string =>
  FLEET.find((a) => new RegExp("(^|[^0-9A-Za-z.:-])" + a.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + "($|[^0-9A-Za-z-])").test(command)) ?? "";
const SYSTEM_PATHS = new Set(["/", "/*", "/etc", "/etc/*", "/usr", "/usr/*", "/var", "/var/*", "/boot", "/lib", "/lib64",
  "/bin", "/sbin", "/home", "/home/*", "/root", "/dev", "/proc", "/sys", "/opt", "/srv"]);
const WRAPPERS = new Set(["sudo", "doas", "nohup", "env", "command", "exec", "time", "nice"]);

// The disk rule (gate.go: diskTools, systemDevice) refuses a disk tool
// only on the disk this machine runs from. The hub reads that from the
// heartbeat's facts; here it is the same findmnt + lsblk -s walk, once,
// when the hook loads. Unknown (the walk failed) means every disk is the
// system disk, as the hub's Check with no machine in hand.
const DISK_TOOLS = new Set(["fdisk", "sfdisk", "parted", "gdisk", "sgdisk", "cfdisk", "wipefs", "mkswap", "dd", "tune2fs", "resize2fs",
  "mdadm", "pvcreate", "vgcreate", "lvcreate", "pvremove", "vgremove", "lvremove", "cryptsetup"]);
const SYSTEM_DISK = "repartitioning or wiping the system disk";
const PSEUDO = new Set(["/dev/null", "/dev/zero", "/dev/random", "/dev/urandom", "/dev/stdin", "/dev/stdout", "/dev/stderr", "/dev/tty"]);
let SYSTEM: string[] | null = null;
try {
  const { execSync } = require("node:child_process");
  const out = execSync(`s() { { for m in / /boot /boot/efi /var /home; do findmnt -no SOURCE "$m" 2>/dev/null; done; awk 'NR>1{print $1}' /proc/swaps; } | sed 's/\\[.*\\]$//' | sort -u; }
{ s; s | while read -r x; do lsblk -lsno NAME "$x" 2>/dev/null | sed 's#^#/dev/#'; done; } | sort -u`, { encoding: "utf8", timeout: 10000, stdio: ["ignore", "pipe", "ignore"] });
  const names = String(out).split("\n").map((x) => x.trim()).filter(Boolean);
  if (names.length) SYSTEM = names;
} catch { SYSTEM = null; }
const systemDevice = (word: string): string => {
  let w = String(word ?? "").replace(/^["']|["']$/g, "");
  if (w.startsWith("if=")) return ""; // dd's input is read, not written
  const eq = w.lastIndexOf("=");
  if (eq >= 0 && w[eq + 1] === "/") w = w.slice(eq + 1);
  if (!w.startsWith("/dev/") || PSEUDO.has(w) || w.startsWith("/dev/fd/")) return "";
  if (SYSTEM === null) return SYSTEM_DISK + " (this machine has not reported which disk carries its system)";
  const name = w.replace(/^\/dev\/mapper\//, "").replace(/^\/dev\//, "");
  if (name.includes("/")) return SYSTEM_DISK + " (name the device by its kernel name, not " + w + ")";
  for (let s of SYSTEM) {
    s = s.slice(s.lastIndexOf("/") + 1);
    if (name === s) return SYSTEM_DISK;
    if (name.startsWith(s)) {
      const rest = name.slice(s.length).replace(/^p/, "");
      if (rest && /^[0-9]+$/.test(rest)) return SYSTEM_DISK;
    }
  }
  return "";
};
const has = (xs: string[], ...any: string[]) => xs.some((x) => any.includes(x));
const anyPath = (xs: string[], ...subs: string[]) => xs.some((x) => subs.some((s) => x.includes(s)));

const RULES: Array<(p: string, a: string[]) => string> = [
  (p, a) => ((p === "ufw" && has(a.slice(0, 1), "disable", "reset")) || (p === "iptables" && has(a, "-F", "--flush")) ||
    (p === "nft" && a.join(" ") === "flush ruleset") ||
    (p === "systemctl" && has(a.slice(0, 1), "stop", "disable", "mask") && has(a.slice(1), "ufw", "firewalld", "nftables"))
    ? "switching off the firewall" : ""),
  (p, a) => ((p === "systemctl" && has(a.slice(0, 1), "stop", "disable", "mask") && has(a.slice(1), "ssh", "sshd", "ssh.socket", "ssh.service", "sshd.service")) ||
    (["rm", "mv", "truncate", "shred", "cp", "install", "chattr", "chmod", "chown"].includes(p) && a.some(holdPath)) ||
    (["deluser", "userdel"].includes(p) && has(a, "homedash", "homedash-agent")) ||
    (p === "passwd" && has(a, "-l") && has(a, "homedash", "homedash-agent")) || (p === "usermod" && has(a, "-L") && has(a, "homedash", "homedash-agent"))
    ? "cutting the hub's own SSH access" : ""),
  (p, a) => (["sed", "perl", "ed", "python", "python3", "tee"].includes(p) && anyPath(a, "/etc/ssh/") ? "editing SSH config by hand" : ""),
  (p, a) => {
    if (!DISK_TOOLS.has(p) && !p.startsWith("mkfs")) return "";
    for (const x of a) { const r = systemDevice(x); if (r) return r; }
    return "";
  },
  (p, a) => {
    if (p !== "rm") return "";
    const recursive = a.some((x) => x === "--recursive" || (x.startsWith("-") && !x.startsWith("--") && /[rR]/.test(x)));
    if (!recursive) return "";
    return a.some((x) => SYSTEM_PATHS.has(x) || SYSTEM_PATHS.has(x.replace(/\/$/, ""))) ? "deleting a system path" : "";
  },
  (p, a) => (["shutdown", "poweroff", "halt", "reboot"].includes(p) || (p === "init" && has(a, "0", "6")) ||
    (p === "systemctl" && has(a, "poweroff", "halt", "reboot", "kexec")) ? "shutting down or rebooting a machine" : ""),
];

export function check(command: string): string {
  const addr = namesFleet(command);
  if (addr) return "reaching the hub or another remote (" + addr + "); ask the hub for a shared workspace instead";
  if (/>>?\s*\/etc\/(ssh\/|sudoers)/.test(command)) return "cutting the hub's own SSH access";
  if (/:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:/.test(command)) return "a fork bomb";
  if (/>>?\s*\/etc\/ssh\//.test(command)) return "editing SSH config by hand";
  for (const m of command.matchAll(/>>?\s*(\/dev\/\S+)/g)) { const r = systemDevice(m[1]); if (r) return r; }
  for (const seg of command.split(/\|\||&&|[;|\n]/)) {
    let words = seg.trim().split(/\s+/).filter(Boolean);
    while (words.length && (WRAPPERS.has(words[0]) || /^[A-Za-z_][A-Za-z0-9_]*=/.test(words[0]) || words[0].startsWith("-"))) words = words.slice(1);
    if (!words.length) continue;
    const prog = words[0].slice(words[0].lastIndexOf("/") + 1);
    for (const r of RULES) {
      const reason = r(prog, words.slice(1));
      if (reason) return reason;
    }
  }
  return "";
}

export default function hook(pi: any): void {
  pi.on("tool_call", async (event: any) => {
    if (event.toolName === "bash") {
      const reason = check(String(event.input?.command ?? ""));
      if (reason) return { block: true, reason: "refused by HomeDash: " + reason };
      return;
    }
    if (["read", "grep", "find", "glob", "ls"].includes(event.toolName)) return;
    const path = event.input?.path ?? event.input?.file_path ?? event.input?.filePath;
    if (path && holdPath(String(path))) return { block: true, reason: "refused by HomeDash: the hub's hold on this machine: " + path };
  });
}
