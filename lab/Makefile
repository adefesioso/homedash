.PHONY: up network vms deploy down prune down-all status
up: ; ./pve-up.sh && ./pve-network.sh && ./vms.sh
network: ; ./pve-network.sh
vms: ; ./vms.sh
deploy: ; ./deploy.sh
down: ; ./down.sh
prune: ; ./prune.sh
down-all: ; ./down.sh --all
status: ; ./ssh.sh pve qm list
