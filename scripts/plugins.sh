#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

#
# This script builds the required plugins.
set -e

BINARY_SUFFIX=""
if [ "${GOOS}x" = "windowsx" ]; then
    BINARY_SUFFIX=".exe"
fi

# Get the parent directory of where this script is.
SOURCE="${BASH_SOURCE[0]}"
while [ -h "$SOURCE" ] ; do SOURCE="$(readlink "$SOURCE")"; done
export DIR="$( cd -P "$( dirname "$SOURCE" )/.." && pwd )"

# Plugin mains are enumerated with a glob instead of the output of ls, since ls
# is free to decorate its output with colours or padding depending on the
# environment and those decorations end up inside the directory name.
build_plugins() {
    local group="$1"
    local prefix="$2"
    local assets="$DIR/plugins/$group/assets"
    local mains=("$DIR"/plugins/"$group"/mains/*)
    local main name

    echo "==> Building ${group} plugins..."
    rm -f "$assets"/dumpling-plugin-*

    for main in "${mains[@]}"; do
        [ -d "$main" ] || continue
        name="$(basename "$main")"
        ( cd "$main" && go build -v -o "$assets/${prefix}${name}${BINARY_SUFFIX}" . )
    done

    if compgen -G "$assets/dumpling-plugin-*" > /dev/null; then
        gzip -f -9 "$assets"/dumpling-plugin-*
    fi
}

build_plugins dumpling dumpling-plugin-
for PLUGIN_TYPE in "kms"; do
    build_plugins "$PLUGIN_TYPE" "dumpling-plugin-${PLUGIN_TYPE}-"
done
