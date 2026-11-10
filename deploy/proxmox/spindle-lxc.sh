#!/usr/bin/env bash
# Spindle in a Proxmox LXC container. Run it in the Proxmox host's shell:
#
#   spindle-lxc.sh                         create a container and start Spindle in it
#   spindle-lxc.sh --restore FILE          the same, starting from a Spindle backup
#   spindle-lxc.sh --update ID             put the newest Spindle into container ID
#
# Options: --id N  --hostname NAME  --storage NAME  --template-storage NAME  --bridge NAME
#          --ip dhcp|ADDRESS/BITS  --gw ADDRESS  --disk GB  --memory MB  --cores N
#          --binary FILE  --url URL  --yes
set -Eeuo pipefail

release_url="https://github.com/mslaby96/spindle/releases/latest/download"
data=/var/lib/spindle
bin=/opt/spindle/spindle
env_file=/etc/spindle/spindle.env

id="" hostname=spindle storage=local-lvm template_storage=local bridge=vmbr0 ip=dhcp gw=""
disk=4 memory=512 cores=1 binary="" url="" restore="" update="" yes=""

say() { printf '\033[1m%s\033[0m\n' "$*" >&2; }
die() { printf 'spindle-lxc: %s\n' "$*" >&2; exit 1; }

usage() {
	sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'
	exit "${1:-0}"
}

parse() {
	while [ $# -gt 0 ]; do
		case "$1" in
		--id) id="$2"; shift ;;
		--hostname) hostname="$2"; shift ;;
		--storage) storage="$2"; shift ;;
		--template-storage) template_storage="$2"; shift ;;
		--bridge) bridge="$2"; shift ;;
		--ip) ip="$2"; shift ;;
		--gw) gw="$2"; shift ;;
		--disk) disk="$2"; shift ;;
		--memory) memory="$2"; shift ;;
		--cores) cores="$2"; shift ;;
		--binary) binary="$2"; shift ;;
		--url) url="$2"; shift ;;
		--restore) restore="$2"; shift ;;
		--update) update="$2"; shift ;;
		--yes) yes=1 ;;
		-h | --help) usage ;;
		*) die "unknown option $1 (see --help)" ;;
		esac
		shift
	done
}

# fetch puts the Spindle binary for this host's architecture in a temporary file and prints its path.
fetch() {
	if [ -n "$binary" ]; then
		[ -f "$binary" ] || die "no such file: $binary"
		echo "$binary"
		return
	fi
	local arch file
	arch="$(dpkg --print-architecture)"
	file="$(mktemp)"
	curl -fsSL "${url:-$release_url/spindle-linux-$arch}" -o "$file" || die "could not download Spindle"
	echo "$file"
}

in_ct() { pct exec "$id" -- "$@"; }

# as_spindle runs a Spindle command with its configuration, the way the service does.
as_spindle() { in_ct runuser -u spindle -- sh -c "set -a; . $env_file; exec $bin $*"; }

wait_for() {
	local what="$1" tries=60
	shift
	until "$@" > /dev/null 2>&1; do
		tries=$((tries - 1))
		[ "$tries" -gt 0 ] || die "$what did not come up"
		sleep 1
	done
}

template() {
	local name
	pveam update > /dev/null || true
	name="$(pveam available --section system | awk '{print $2}' | grep '^debian-12-standard_' | sort -V | tail -1)"
	[ -n "$name" ] || die "no Debian 12 template is available"
	if ! pveam list "$template_storage" | grep -q "$name"; then
		say "Downloading $name"
		pveam download "$template_storage" "$name" > /dev/null
	fi
	echo "$template_storage:vztmpl/$name"
}

host_timezone() {
	timedatectl show -p Timezone --value 2> /dev/null || cat /etc/timezone 2> /dev/null || echo UTC
}

ask_token() {
	local token=""
	printf 'Discogs personal access token (Enter to add it later): ' >&2
	if [ -t 0 ]; then
		read -r -s token || true
		echo >&2
	else
		read -r token || true
	fi
	echo "$token"
}

write_config() {
	local token="$1" dir="$2"
	{
		echo "SPINDLE_DATA_DIR=$data"
		echo "SPINDLE_TZ=$(host_timezone)"
		echo "# Personal access token from https://www.discogs.com/settings/developers"
		if [ -n "$token" ]; then echo "SPINDLE_DISCOGS_TOKEN=$token"; else echo "#SPINDLE_DISCOGS_TOKEN="; fi
		echo "# Home Assistant: SPINDLE_MQTT_URL=mqtt://broker:1883, SPINDLE_MQTT_USERNAME, SPINDLE_MQTT_PASSWORD"
	} > "$dir/spindle.env"
	cat > "$dir/spindle.service" <<-EOF
		[Unit]
		Description=Spindle
		Wants=network-online.target
		After=network-online.target

		[Service]
		User=spindle
		Group=spindle
		EnvironmentFile=$env_file
		ExecStart=$bin serve
		Restart=on-failure
		RestartSec=5
		NoNewPrivileges=yes
		ProtectSystem=strict
		ProtectHome=yes
		PrivateTmp=yes
		ReadWritePaths=$data

		[Install]
		WantedBy=multi-user.target
	EOF
}

confirm() {
	[ -n "$yes" ] && return
	say "About to create container $id ($hostname): $cores core(s), $memory MB, $disk GB on $storage, $bridge, ip=$ip"
	printf 'Go ahead? [y/N] ' >&2
	local answer
	read -r answer
	[ "$answer" = y ] || [ "$answer" = Y ] || die "stopped, nothing was created"
}

create() {
	local file tmpl token work net
	id="${id:-$(pvesh get /cluster/nextid)}"
	confirm
	file="$(fetch)"
	token="$(ask_token)"
	tmpl="$(template)"
	net="name=eth0,bridge=$bridge,ip=$ip${gw:+,gw=$gw}"

	say "Creating container $id"
	pct create "$id" "$tmpl" --hostname "$hostname" --cores "$cores" --memory "$memory" --swap 256 \
		--rootfs "$storage:$disk" --net0 "$net" --unprivileged 1 --features nesting=1 --onboot 1 \
		--timezone host --tags spindle > /dev/null
	pct start "$id"
	wait_for "the container" in_ct true

	say "Installing Spindle"
	in_ct useradd --system --home-dir "$data" --shell /usr/sbin/nologin spindle
	in_ct mkdir -p /opt/spindle "$data" /etc/spindle
	in_ct chown spindle:spindle "$data"
	pct push "$id" "$file" "$bin" --perms 0755
	work="$(mktemp -d)"
	write_config "$token" "$work"
	pct push "$id" "$work/spindle.env" "$env_file" --perms 0640 --group spindle
	pct push "$id" "$work/spindle.service" /etc/systemd/system/spindle.service --perms 0644
	rm -rf "$work"

	if [ -n "$restore" ]; then
		say "Restoring $(basename "$restore")"
		pct push "$id" "$restore" "$data/restore.tar.gz" --user spindle --group spindle
		as_spindle restore "$data/restore.tar.gz"
		in_ct rm -f "$data/restore.tar.gz"
	fi

	in_ct systemctl daemon-reload
	in_ct systemctl enable --now spindle
	wait_for "Spindle" in_ct "$bin" healthcheck
	report
}

report() {
	local address code
	address="$(in_ct hostname -I | awk '{print $1}')"
	say "Spindle is running: http://$address:8080"
	if [ -z "$restore" ]; then
		code="$(in_ct journalctl -u spindle -o cat | grep -o 'code=[A-Z0-9]*' | tail -1 | cut -d= -f2)"
		[ -n "$code" ] && say "Create your account there with the setup code $code (it works for an hour)."
	fi
	say "Settings: pct exec $id -- nano $env_file, then pct exec $id -- systemctl restart spindle"
}

upgrade() {
	local file
	id="$update"
	file="$(fetch)"
	say "Backing up container $id before the update"
	as_spindle backup
	pct push "$id" "$file" "$bin.new" --perms 0755
	in_ct mv "$bin.new" "$bin"
	in_ct systemctl restart spindle
	wait_for "Spindle" in_ct "$bin" healthcheck
	say "Spindle in container $id is updated."
}

main() {
	parse "$@"
	command -v pveversion > /dev/null || die "run this in the shell of a Proxmox host"
	if [ -n "$update" ]; then upgrade; else create; fi
}

main "$@"
