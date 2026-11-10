#!/usr/bin/env bash
# Runs spindle-lxc.sh against stand-ins for the Proxmox tools and checks what it would have done.
set -Euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
failures=0

setup() {
	work="$(mktemp -d)"
	mkdir -p "$work/bin" "$work/pushed"
	log="$work/calls.log"
	: > "$log"
	for tool in pct pveam pvesh pveversion; do
		cat > "$work/bin/$tool" <<STUB
#!/usr/bin/env bash
echo "$tool \$*" >> "$log"
case "$tool \$1" in
	"pvesh get") echo 105 ;;
	"pveam available") printf 'system          debian-12-standard_12.7-1_amd64.tar.zst\nsystem          debian-11-standard_11.7-1_amd64.tar.zst\n' ;;
	"pveam list") echo "NAME SIZE" ;;
	"pct push") cp "\$3" "$work/pushed/\$(basename "\$4")" ;;
	"pct exec")
		case "\$*" in
			*"hostname -I"*) echo "192.168.20.50 " ;;
			*journalctl*) echo 'msg="no account yet" code=K7F29QX2AB' ;;
		esac ;;
esac
exit 0
STUB
		chmod +x "$work/bin/$tool"
	done
	printf '#!/usr/bin/env bash\necho amd64\n' > "$work/bin/dpkg"
	chmod +x "$work/bin/dpkg"
	printf 'fake binary' > "$work/spindle-linux-amd64"
}

run() { PATH="$work/bin:$PATH" bash "$here/spindle-lxc.sh" "$@" > "$work/out.txt" 2>&1; }

expect() {
	local what="$1" pattern="$2" file="${3:-$log}"
	if ! grep -qE -- "$pattern" "$file"; then
		echo "FAIL: $what"
		echo "  expected /$pattern/ in $(basename "$file"):"
		sed 's/^/    /' "$file"
		failures=$((failures + 1))
	fi
}

refuse() {
	local what="$1" pattern="$2" file="${3:-$log}"
	if grep -qE -- "$pattern" "$file"; then
		echo "FAIL: $what"
		failures=$((failures + 1))
	fi
}

# a new container from the newest Debian 12 template, unprivileged, with Spindle running as a service
setup
printf 'tok123\n' | run --yes --binary "$work/spindle-linux-amd64"
expect "downloads the Debian 12 template" "pveam download local debian-12-standard_12.7-1_amd64.tar.zst"
expect "creates an unprivileged container" "pct create 105 local:vztmpl/debian-12-standard_12.7-1_amd64.tar.zst .*--unprivileged 1"
expect "enables nesting for systemd's sandboxing" "pct create .*--features nesting=1"
expect "starts at boot" "pct create .*--onboot 1"
expect "uses DHCP by default" "pct create .*ip=dhcp"
expect "pushes the binary" "pct push 105 .*spindle-linux-amd64 /opt/spindle/spindle"
expect "installs the service" "pct push 105 .* /etc/systemd/system/spindle.service"
expect "enables and starts it" "pct exec 105 -- systemctl enable --now spindle"
expect "writes the Discogs token into the config" "SPINDLE_DISCOGS_TOKEN=tok123" "$work/pushed/spindle.env"
expect "keeps the data in /var/lib/spindle" "SPINDLE_DATA_DIR=/var/lib/spindle" "$work/pushed/spindle.env"
expect "runs as its own user" "User=spindle" "$work/pushed/spindle.service"
expect "tells where to open it" "http://192.168.20.50:8080" "$work/out.txt"
expect "shows the setup code" "K7F29QX2AB" "$work/out.txt"
refuse "never prints the token" "tok123" "$work/out.txt"
rm -rf "$work"

# a static address and other sizes
setup
printf '\n' | run --yes --binary "$work/spindle-linux-amd64" --id 120 --ip 192.168.20.60/24 --gw 192.168.20.1 --storage fast --memory 1024
expect "takes the given id and storage" "pct create 120 .*--rootfs fast:4"
expect "uses the static address" "ip=192.168.20.60/24,gw=192.168.20.1"
expect "takes the memory" "--memory 1024"
refuse "a token left empty is not written" "SPINDLE_DISCOGS_TOKEN=." "$work/pushed/spindle.env"
rm -rf "$work"

# restoring a backup instead of starting empty
setup
printf 'backup' > "$work/spindle-backup.tar.gz"
printf '\n' | run --yes --binary "$work/spindle-linux-amd64" --restore "$work/spindle-backup.tar.gz"
expect "pushes the backup" "pct push 105 .*spindle-backup.tar.gz /var/lib/spindle/restore.tar.gz"
expect "restores it as the spindle user before the first start" "pct exec 105 -- runuser -u spindle -- .*/opt/spindle/spindle restore /var/lib/spindle/restore.tar.gz"
refuse "no setup code after a restore" "K7F29QX2AB" "$work/out.txt"
if grep -n "restore /var/lib" "$log" | head -1 | grep -q . && grep -n "enable --now" "$log" | head -1 | grep -q .; then
	r=$(grep -n "restore /var/lib" "$log" | head -1 | cut -d: -f1)
	s=$(grep -n "enable --now" "$log" | head -1 | cut -d: -f1)
	[ "$r" -lt "$s" ] || { echo "FAIL: restores before starting"; failures=$((failures + 1)); }
fi
rm -rf "$work"

# updating an existing container: backup first, then the new binary, then a restart
setup
run --update 105 --binary "$work/spindle-linux-amd64"
expect "backs up first" "pct exec 105 -- runuser -u spindle -- .*/opt/spindle/spindle backup"
expect "swaps the binary" "pct exec 105 -- mv /opt/spindle/spindle.new /opt/spindle/spindle"
expect "restarts" "pct exec 105 -- systemctl restart spindle"
refuse "creates nothing" "pct create"
rm -rf "$work"

# refuses to run anywhere but on Proxmox
setup
rm "$work/bin/pveversion"
PATH="$work/bin:/usr/bin:/bin" bash "$here/spindle-lxc.sh" --yes --binary "$work/spindle-linux-amd64" > "$work/out.txt" 2>&1 && {
	echo "FAIL: ran without Proxmox"
	failures=$((failures + 1))
}
expect "says why" "Proxmox" "$work/out.txt"
rm -rf "$work"

if [ "$failures" -gt 0 ]; then
	echo "$failures failed"
	exit 1
fi
echo "all passed"
