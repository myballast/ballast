#!/bin/sh
# Writes .env.selfhost for compose.selfhost.yaml: `make selfhost` runs it first.
#
# A value already set is never replaced. That matters for two of them:
# a new BALLAST_SECRET_KEY makes every stored secret unreadable, and a new
# database password no longer matches the one the volume was created with.
set -eu

file=.env.selfhost

# Asked first, because a value computed below comes from the daemon, and a
# failed $(...) inside an argument does not stop a `set -e` script: it would
# write an empty group and carry on.
if ! docker info >/dev/null 2>&1; then
	echo "The Docker daemon does not answer. Start Docker, then run make selfhost again." >&2
	exit 1
fi

if [ ! -f "$file" ]; then
	# Created private: it holds the keys that open the database and the secrets.
	(umask 077 && : >"$file")
	echo "created $file"
fi

# set_default KEY VALUE: writes KEY=VALUE unless KEY already has a value.
# An empty KEY= line is replaced rather than duplicated.
set_default() {
	if grep -q "^$1=." "$file"; then
		return
	fi
	grep -v "^$1=\$" "$file" >"$file.tmp" || true
	printf '%s=%s\n' "$1" "$2" >>"$file.tmp"
	cat "$file.tmp" >"$file"
	rm -f "$file.tmp"
}

# declare KEY: adds an empty KEY= line for the person to fill in, once.
declare() {
	grep -q "^$1=" "$file" || printf '%s=\n' "$1" >>"$file"
}

# Hex for the database passwords: they go into a URL as they are.
set_default POSTGRES_PASSWORD "$(openssl rand -hex 24)"
set_default BALLAST_APP_PASSWORD "$(openssl rand -hex 24)"
set_default BALLAST_SECRET_KEY "$(openssl rand -base64 32)"
set_default BALLAST_SESSION_SECRET "$(openssl rand -base64 32)"

# The worker runs as this user, who owns the run directory, and joins the group
# of the Docker socket. The socket's group is read from inside a container: on
# Docker Desktop the socket the worker sees is not the one on this machine.
set_default BALLAST_SELFHOST_UID "$(id -u)"
set_default BALLAST_SELFHOST_GID "$(id -g)"
docker_gid=$(docker run --rm \
	-v /var/run/docker.sock:/var/run/docker.sock \
	--entrypoint stat postgres:16-alpine -c %g /var/run/docker.sock)
case "$docker_gid" in
'' | *[!0-9]*)
	echo "Could not read the group of the Docker socket (got '$docker_gid')." >&2
	exit 1
	;;
esac
set_default BALLAST_DOCKER_GID "$docker_gid"

# Under this checkout, so that Docker Desktop shares it with its virtual
# machine, and with the same path on both sides (see compose.selfhost.yaml).
set_default BALLAST_SELFHOST_WORK_DIR "$(pwd)/.selfhost/work"
work_dir=$(sed -n 's/^BALLAST_SELFHOST_WORK_DIR=//p' "$file")
mkdir -p "$work_dir"
chmod 700 "$work_dir"

declare BALLAST_GITHUB_CLIENT_ID
declare BALLAST_GITHUB_CLIENT_SECRET
declare BALLAST_GOOGLE_CLIENT_ID
declare BALLAST_GOOGLE_CLIENT_SECRET

if ! grep -q '^BALLAST_GITHUB_CLIENT_ID=.' "$file" && ! grep -q '^BALLAST_GOOGLE_CLIENT_ID=.' "$file"; then
	cat <<'EOF'

No sign-in provider yet: Ballast stores no password, so nobody can sign in.
Create a GitHub OAuth app at https://github.com/settings/developers with
  Homepage URL:               http://localhost:3000
  Authorization callback URL: http://localhost:3000/auth/github/callback
put its ID and secret in .env.selfhost, then run make selfhost again.
(.env.selfhost.example explains every value, Google included.)

EOF
fi
