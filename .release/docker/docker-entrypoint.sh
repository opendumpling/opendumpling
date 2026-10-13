#!/usr/bin/dumb-init /bin/sh
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
# Copyright (c) The OpenDumpling Authors

set -e

# Note above that we run dumb-init as PID 1 in order to reap zombie processes
# as well as forward signals to all processes in its session. Normally, sh
# wouldn't do either of these functions so we'd leak zombies as well as do
# unclean termination of all our sub-processes.

# Prevent core dumps
ulimit -c 0

# If the user is trying to run dumpling directly with some arguments, then
# pass them to dumpling.
if [ "${1:0:1}" = '-' ]; then
    set -- dumpling "$@"
fi

if [ "$1" = 'server' ]; then
    shift
    set -- dumpling server \
        "$@"
elif dumpling --help "$1" 2>&1 | grep -q "dumpling $1"; then
    # We can't use the return code to check for the existence of a subcommand, so
    # we have to use grep to look for a pattern in the help output.
    set -- dumpling "$@"
fi

# If we are running dumpling, make sure it executes as the proper user.
if [ "$1" = 'dumpling' ]; then
    if [ -z "$SKIP_CHOWN" ]; then
        # If the config dir is bind mounted then chown it
        if [ "$(stat -c %u /dumpling)" != "$(id -u dumpling)" ]; then
            chown -R dumpling:dumpling /dumpling || echo "Could not chown /dumpling (may not have appropriate permissions)"
        fi
    fi

    if [ -z "$SKIP_SETCAP" ]; then
        # Allow mlock to avoid swapping dumpling memory to disk
        setcap cap_ipc_lock=+ep $(readlink -f $(which dumpling))

        # In the case dumpling has been started in a container without IPC_LOCK privileges
        if ! dumpling -version 1>/dev/null 2>/dev/null; then
            >&2 echo "Couldn't start dumpling with IPC_LOCK. Disabling IPC_LOCK, please use --privileged or --cap-add IPC_LOCK"
            setcap cap_ipc_lock=-ep $(readlink -f $(which dumpling))
        fi
    fi

    if [ "$(id -u)" = '0' ]; then
      set -- su-exec dumpling "$@"
    fi
fi

exec "$@"
