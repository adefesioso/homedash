package gate

import (
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	refused := []string{
		"ufw disable",
		"sudo systemctl stop ssh",
		"rm -rf /",
		"rm -rf /etc",
		"rm -rf / --no-preserve-root",
		"echo x >> /etc/ssh/sshd_config",
		"sed -i 's/PermitRootLogin no/yes/' /etc/ssh/sshd_config",
		"mkfs.ext4 /dev/sdb1",
		"dd if=/dev/zero of=/dev/sda bs=1M",
		"reboot",
		"sudo shutdown -h now",
		"apt update && systemctl reboot",
		"userdel homedash",
		"rm ~/.ssh/authorized_keys",
	}
	for _, c := range refused {
		if err := Check(c); err == nil {
			t.Errorf("not refused: %q", c)
		} else if !IsRefusal(err) {
			t.Errorf("wrong error kind for %q: %v", c, err)
		}
	}
	allowed := []string{
		"docker compose up -d",
		"rm -rf /home/homedash/stacks/jellyfin",
		"rm -rf ./build",
		"systemctl restart jellyfin",
		"apt-get install -y mergerfs",
		"df -h /",
		"cat /etc/ssh/sshd_config",
		"echo reboot-later",
		"ollama pull qwen2.5:0.5b",
	}
	for _, c := range allowed {
		if err := Check(c); err != nil {
			t.Errorf("wrongly refused %q: %v", c, err)
		}
	}
}

func TestComposeRefusal(t *testing.T) {
	refused := map[string]string{
		"services:\n  x:\n    image: a\n    privileged: true\n":                                                                       "privileged",
		"services:\n  x:\n    image: a\n    pid: host\n":                                                                              "PID",
		"services:\n  x:\n    image: a\n    network_mode: host\n":                                                                     "network",
		"services:\n  x:\n    image: a\n    cap_add:\n      - SYS_ADMIN\n":                                                            "capability",
		"services:\n  x:\n    image: a\n    cap_add: [ALL]\n":                                                                         "capability",
		"services:\n  x:\n    image: a\n    volumes:\n      - /var/run/docker.sock:/var/run/docker.sock\n":                            "bind mount",
		"services:\n  x:\n    image: a\n    volumes:\n      - /:/host\n":                                                              "bind mount",
		"services:\n  x:\n    image: a\n    volumes:\n      - type: bind\n        source: /var/run/docker.sock\n        target: /s\n": "bind mount",
	}
	for c, want := range refused {
		got := ComposeRefusal(c)
		if got == "" {
			t.Errorf("ComposeRefusal(%q) = \"\", want a refusal (%s)", c, want)
			continue
		}
		if !strings.Contains(got, want) {
			t.Errorf("ComposeRefusal(%q) = %q, want to contain %q", c, got, want)
		}
	}
	ok := []string{
		"services:\n  web:\n    image: traefik/whoami\n    ports:\n      - \"8080:80\"\n",
		"services:\n  x:\n    image: a\n    cap_add:\n      - NET_ADMIN\n",
		"services:\n  x:\n    image: a\n    volumes:\n      - /srv/photos:/data\n",
		"services:\n  x:\n    image: a\n    volumes:\n      - photos:/data\nvolumes:\n  photos: {}\n",
	}
	for _, c := range ok {
		if got := ComposeRefusal(c); got != "" {
			t.Errorf("ComposeRefusal(%q) = %q, want \"\"", c, got)
		}
	}
}

// The disk rule with a machine in hand: the system disk and everything
// on it is refused; a data disk is the job's to format, partition and
// mount.
func TestSystemDisk(t *testing.T) {
	system := []string{"/dev/sda", "/dev/sda1", "/dev/sda2", "/dev/mapper/vg-root", "/dev/vg-root", "/swap.img"}
	refused := []string{
		"mkfs.ext4 /dev/sda",
		"mkfs.ext4 -F /dev/sda2",
		"sudo parted -s /dev/sda mklabel gpt",
		"wipefs -a /dev/mapper/vg-root",
		"dd if=/dev/zero of=/dev/sda1 bs=1M",
		"cat /dev/zero > /dev/sda",
		"mkfs.ext4 /dev/disk/by-id/ata-something",
		"lvremove /dev/vg/root",
		"tune2fs -O ^has_journal /dev/sda2",
	}
	for _, c := range refused {
		if err := CheckOn(c, system); err == nil || !IsRefusal(err) {
			t.Errorf("not refused on the system disk: %q: %v", c, err)
		}
	}
	allowed := []string{
		"mkfs.ext4 -L pool /dev/sdb",
		"parted -s /dev/sdc mklabel gpt mkpart primary ext4 0% 100%",
		"wipefs -a /dev/sdb && mkfs.ext4 /dev/sdb",
		"mkfs.ext4 /dev/sdab",
		"mkfs.xfs /dev/nvme1n1p1",
		"dd if=/dev/zero of=/dev/sdb bs=1M count=10",
		"mkdir -p /mnt/sdb && mount /dev/sdb /mnt/sdb && printf '/dev/sdb /mnt/sdb ext4 defaults,nofail 0 0\\n' | tee -a /etc/fstab && systemctl daemon-reload && mount -a",
		"blkid /dev/sdb",
	}
	for _, c := range allowed {
		if err := CheckOn(c, system); err != nil {
			t.Errorf("wrongly refused on a data disk: %q: %v", c, err)
		}
	}
	// No machine in hand: every disk is the system disk, as before.
	for _, c := range []string{"mkfs.ext4 /dev/sdb", "parted -s /dev/sdc print", "cat /dev/zero > /dev/sdb"} {
		if err := Check(c); err == nil {
			t.Errorf("not refused with no machine in hand: %q", c)
		}
	}
	// A partition of a system disk under nvme naming.
	if err := CheckOn("mkfs.ext4 /dev/nvme0n1p3", []string{"/dev/nvme0n1", "/dev/nvme0n1p2"}); err == nil {
		t.Error("nvme partition of the system disk not refused")
	}
}
