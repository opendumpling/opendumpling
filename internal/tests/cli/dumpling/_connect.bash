# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

function connect_nc() {
  local id=$1
  # Note: When this command returns, the session immediately goes into a "canceling" state
  echo "foo" | dumpling connect -exec nc -target-id $id -- {{dumpling.ip}} {{dumpling.port}}
}
