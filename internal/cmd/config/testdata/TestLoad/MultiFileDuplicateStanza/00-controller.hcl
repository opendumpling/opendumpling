# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

disable_mlock = true

controller {
  name        = "controller0"
  description = "A controller"

  database {
    url = "env://LOAD_TEST_DUMPLING_POSTGRES_URL"
  }
}

listener "tcp" {
  address     = "dumpling"
  purpose     = "api"
  tls_disable = true
}

listener "tcp" {
  address     = "dumpling"
  purpose     = "cluster"
  tls_disable = true
}

listener "tcp" {
  address     = "dumpling"
  purpose     = "ops"
  tls_disable = true
}
