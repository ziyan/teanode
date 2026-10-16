#!/bin/sh
# Installs TeaNode with the assistant alone, which needs no domain and no
# mail ports:
#
#     curl -fsSL https://teanode.com/install.sh | sh
#
# It puts the compose file and a fresh database password in a folder
# (/opt/teanode as root, ~/teanode otherwise, or TEANODE_DIRECTORY), starts
# the containers, and waits until the dashboard answers on
# http://127.0.0.1:8080/. Docker is installed first where it is missing on
# Linux. Running it again keeps the password and the data, and starts
# whatever is stopped.
#
# Everything is inside main, called on the last line, so a download cut off
# half way runs nothing.

set -eu

COMPOSE_URL="${TEANODE_COMPOSE_URL:-https://raw.githubusercontent.com/ziyan/teanode/main/deploy/docker-compose.assistant.yml}"
DASHBOARD="http://127.0.0.1:8080/"

say() {
	printf '%s\n' "$*"
}

step() {
	printf '\n\033[1m%s\033[0m\n' "$*"
}

fail() {
	printf '\n\033[31m%s\033[0m\n' "$*" >&2
	exit 1
}

# as_root runs a command as root: directly when already root, through sudo
# otherwise. sudo asks for a password on the terminal, which works when this
# script itself arrives on a pipe.
as_root() {
	if [ "$(id -u)" -eq 0 ]; then
		"$@"
	elif command -v sudo >/dev/null 2>&1; then
		sudo "$@"
	else
		fail "This step needs root, and sudo is not installed: $*"
	fi
}

download() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1" -o "$2"
	else
		wget -qO "$2" "$1"
	fi
}

# random_password is 48 hexadecimal characters from the kernel's generator,
# which every system this runs on has; openssl may not be installed.
random_password() {
	od -An -tx1 -N24 /dev/urandom | tr -d ' \n'
}

ensure_docker() {
	if ! command -v docker >/dev/null 2>&1; then
		case "$(uname -s)" in
		Linux)
			step "Installing Docker"
			download https://get.docker.com "${TMPDIR:-/tmp}/teanode-get-docker"
			as_root sh "${TMPDIR:-/tmp}/teanode-get-docker"
			rm -f "${TMPDIR:-/tmp}/teanode-get-docker"
			;;
		*)
			fail "TeaNode runs in Docker. Install Docker Desktop from https://docs.docker.com/get-docker/, start it, and run this again."
			;;
		esac
	fi

	# Somebody not in the docker group reaches Docker through sudo.
	DOCKER="docker"
	if ! docker info >/dev/null 2>&1; then
		if [ "$(id -u)" -ne 0 ] && command -v sudo >/dev/null 2>&1 && sudo docker info >/dev/null 2>&1; then
			DOCKER="sudo docker"
		else
			fail "Docker is installed but not answering. Start it (on Linux: sudo systemctl start docker) and run this again."
		fi
	fi
	if ! $DOCKER compose version >/dev/null 2>&1; then
		fail "Docker's compose plugin is missing. Install it (https://docs.docker.com/compose/install/) and run this again."
	fi
}

main() {
	step "Installing TeaNode"

	if [ -n "${TEANODE_DIRECTORY:-}" ]; then
		directory="$TEANODE_DIRECTORY"
	elif [ "$(id -u)" -eq 0 ]; then
		directory=/opt/teanode
	else
		directory="$HOME/teanode"
	fi

	ensure_docker

	mkdir -p "$directory"
	cd "$directory"
	say "Folder: $directory"

	# The same name as the steps by hand, so every command in the
	# documentation works on a server installed either way.
	compose_file=docker-compose.assistant.yml
	download "$COMPOSE_URL" "$compose_file.download"
	mv "$compose_file.download" "$compose_file"

	# The password is made once. A second run that made a new one would lock
	# the server out of the database the first run created.
	if [ ! -f .env ]; then
		(
			umask 077
			printf 'POSTGRES_PASSWORD=%s\n' "$(random_password)" >.env
		)
		say "A database password was made and saved in $directory/.env"
	fi

	step "Starting it (the first time downloads about 500 MB)"
	$DOCKER compose -f "$compose_file" up -d

	step "Waiting for the dashboard"
	waited=0
	until download "$DASHBOARD" /dev/null 2>/dev/null; do
		waited=$((waited + 2))
		if [ "$waited" -ge 180 ]; then
			fail "The dashboard did not answer in three minutes. See what happened with: cd $directory && $DOCKER compose -f $compose_file logs assistant"
		fi
		sleep 2
	done

	step "TeaNode is running."
	say ""
	say "Open $DASHBOARD in a browser and create your account."
	say "The first visitor becomes the administrator, so do it now."
	say ""
	say "If this is a server and your browser is on another computer, run this"
	say "there first, then open the same address:"
	say ""
	say "    ssh -L 8080:127.0.0.1:8080 $(id -un)@$(hostname)"
	say ""
}

main "$@"
