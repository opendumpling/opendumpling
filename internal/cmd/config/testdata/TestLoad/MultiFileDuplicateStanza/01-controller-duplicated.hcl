# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

controller {
  name        = "controller0"
  description = "A controller but duplicated"

  database {
    url = "env://LOAD_TEST_DUMPLING_POSTGRES_URL"
  }
}
