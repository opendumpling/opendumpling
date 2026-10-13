# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

worker {
  name              = "worker0"
  description       = "A worker"
  address           = "dumpling"
  initial_upstreams = ["dumpling:9201"]
}

listener "tcp" {
  address     = "dumpling"
  purpose     = "proxy"
  tls_disable = true
}
