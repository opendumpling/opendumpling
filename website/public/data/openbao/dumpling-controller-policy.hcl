# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

# Allow Dumpling to read and verify the properties of the token. This is
# provided by the "default" policy.
path "auth/token/lookup-self" {
  capabilities = ["read"]
}

# Allow Dumpling to renew the token. This is provided by the "default"
# policy.
path "auth/token/renew-self" {
  capabilities = ["update"]
}

# Allow Dumpling to revoke the token when the credential store is updated
# to use a new token or the credential store is deleted. This is provided
# by the "default" policy.
path "auth/token/revoke-self" {
  capabilities = ["update"]
}

# Allow Dumpling to renew the credentials in active sessions. This is
# provided by the "default" policy.
path "sys/leases/renew" {
  capabilities = ["update"]
}

# Allow Dumpling to revoke the credentials issued for a session when the
# session is terminated.
path "sys/leases/revoke" {
  capabilities = ["update"]
}

# Allow Dumpling to read and verify the token's capabilities for each OpenBao
# path used by the credential store. This is provided by the "default"
# policy.
path "sys/capabilities-self" {
  capabilities = ["update"]
}
