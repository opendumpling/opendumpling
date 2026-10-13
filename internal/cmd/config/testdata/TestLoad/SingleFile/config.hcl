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

worker {
  name              = "worker0"
  description       = "A worker"
  address           = "dumpling"
  initial_upstreams = ["dumpling:9201"]
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
  purpose     = "proxy"
  tls_disable = true
}

listener "tcp" {
  address     = "dumpling"
  purpose     = "ops"
  tls_disable = true
}

kms "aead" {
  purpose   = "root"
  aead_type = "aes-gcm"
  key       = "sP1fnF5Xz85RrXyELHFeZg9Ad2qt4Z4bgNHVGtD6ung="
  key_id    = "global_root"
}

kms "aead" {
  purpose   = "worker-auth"
  aead_type = "aes-gcm"
  key       = "8fZBjCUfN0TzjEGLQldGY4+iE9AkOvCfjh7+p0GtRBQ="
  key_id    = "global_worker-auth"
}

kms "aead" {
  purpose   = "recovery"
  aead_type = "aes-gcm"
  key       = "8fZBjCUfN0TzjEGLQldGY4+iE9AkOvCfjh7+p0GtRBQ="
  key_id    = "global_recovery"
}

reporting {
	license {
		enabled = false
  }
}
