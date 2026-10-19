# Registering workers with a cluster id

This document describes how a worker finds a Dumpling cluster and how it is
registered with it, in the order an operator goes through them. It mirrors the
flow a managed control plane uses: a worker is configured with nothing but the
id of the cluster it belongs to, it prints a registration request, and that
request is what authorizes it.

The alternative, listing upstream addresses directly, is described in
`docs/multihop-ssh-testrunbook.md` and still works exactly as before.

## Two things a worker has to get right

A worker has to be told *where* its cluster is and it has to *authenticate* to
it. Those are separate, and the two ways of authenticating are mutually
exclusive, which is what makes the choice below worth spelling out.

| | With a `worker-auth` KMS stanza | With an `auth_storage_path` |
|---|---|---|
| Identity | the `name` in its configuration | a registration request, registered once |
| Registration | none | `dumpling workers register` |
| `auth_storage_path` | must not be set | must be set |
| `name` in config | required | must not be set |
| Can wrap the registration of workers connecting to it | yes | no |

The left column is the shared-key method, convenient on a single network where
the same key is distributed to the controller and every worker. The right
column is the node enrollment method, and it is the one that scales: a worker
behind NAT anywhere in the world can be registered with a token and needs
nothing from you but a reachable address. The cluster id below is only about
finding the cluster, so it works with either.

## Telling a worker where its cluster is

A worker needs one upstream. There are two ways to give it one.

### With a cluster id

```hcl
cluster_id = "c-4f9a1c2e"
```

The worker resolves `<cluster id>.<discovery domain>` in DNS and dials every
address it finds, the way a worker discovers the control plane of its own
cluster without being told an address. This is what makes a worker
configurable without knowing anything about the network it runs in, which is
the point of a cluster id.

`discovery_domain` defaults to `dumpling.local`. A cluster which publishes
itself under a different name says so:

```hcl
cluster_id = "c-4f9a1c2e"
discovery_domain        = "dumpling.example.com"
```

with, for instance, a single wildcard record pointing at whatever hosts the
cluster:

```
*.dumpling.example.com.  300  IN  A  203.0.113.10
```

The discovered addresses get the default worker port, `9201`, the port of a
controller's cluster listener. Discovery carries no port information.

A cluster id does not have to be a UUID; any printable string works, so an
operator can use something meaningful such as `prod-eu-1`. It does have to
match what the worker is configured with and what the cluster is configured
with, since the worker is trusting whatever that name resolves to.

### With a fallback address

DNS is not always available, and a cluster reachable only by a literal address
is a perfectly normal thing. `discovery_fallback_addr` is used when the lookup
fails:

```hcl
cluster_id  = "c-4f9a1c2e"
discovery_fallback_addr  = ["dumpling.example.com:9201"]
```

The fallback is only consulted when a cluster id is set, so a worker never
quietly joins some other cluster than the one it was told about. It accepts the
same forms as `initial_upstreams`: a list of addresses, or a single string
holding a JSON array which may be read from an environment variable or a file.

```hcl
cluster_id = "c-4f9a1c2e"
discovery_fallback_addr = "env://DUMPLING_UPSTREAMS"
```

Setting neither `discovery_domain` nor `discovery_fallback_addr` is allowed but
warned about at start up: the worker can then only reach its cluster when
`<cluster id>.dumpling.local` happens to resolve.

### With a list of addresses

```hcl
worker {
  initial_upstreams = ["dumpling.example.com:9201"]
}
```

This is the older form and it is mutually exclusive with `cluster_id`.
Use it for a worker which joins through another worker rather than through the
cluster itself, which is how the downstream workers of a multi-hop route are
configured.

## Registering the worker

A worker which is going to be registered is configured with a storage path and
without a `worker-auth` KMS stanza or a name, since both of those would mean
authenticating by key instead:

```hcl
cluster_id = "c-4f9a1c2e"
discovery_fallback_addr = ["dumpling.example.com:9201"]

disable_mlock = true

listener "tcp" {
  address = "0.0.0.0"
  purpose = "proxy"
}

worker {
  auth_storage_path = "/opt/dumpling/ingress-1"
}
```

Start the worker:

```sh
dumpling server -config worker.hcl
```

It logs a cluster id if it has one, and it writes its **Worker Auth
Registration Request** both to the log and to `auth_request_token` in its
`auth_storage_path`. The registration request is what proves the worker exists;
nothing about it is a secret that has to be protected in transit, but it does
authorize whoever holds it to add that worker, so treat it like a credential
while it is current.

Register it, pointing at the file rather than copying the token out of a log:

```sh
export DUMPLING_ADDR=https://dumpling.example.com:9200
dumpling workers register \
  -name ingress-1 \
  -auth-request-file /opt/dumpling/ingress-1/auth_request_token
```

`-auth-request-file -` reads the request from standard input, and
`DUMPLING_WORKER_AUTH_REQUEST_FILE` can be used instead of the flag. The scope
defaults to `global`, which is where workers live.

The worker connects on its next status report; it does not need to be
restarted, and no activation token has to be copied back into its
configuration. The `auth_request_token` file is removed by the worker once it
has authenticated.

Tags are set after registration, the same way as for any other worker:

```sh
dumpling workers add-worker-tags -id w_1234567890 -tag "type=ingress"
```

## Controller-led registration

`dumpling workers create controller-led` is the other direction: the operator
creates the worker in Dumpling first and receives a
`controller_generated_activation_token` to put in the worker configuration.
Worker-led registration, above, is the shorter of the two and is what the rest
of the documentation uses.

## Notes and limitations

- The cluster id is a discovery key, not an authorization decision. What
  actually binds a worker to a cluster is the cluster's node enrollment trust
  root: a worker registered with one cluster cannot authenticate to another,
  even if it is handed its address.
- A worker whose lookup fails and which has no fallback address fails to start
  with an error naming the address it tried, rather than starting in a state
  where it can never reach anything.
- Discovery is a plain A/AAAA lookup. It does not carry a port, so a cluster
  whose worker port is not `9201` needs a fallback address.
- When the connection to the controllers is lost and re-established, a worker
  with a cluster id re-resolves it rather than replaying whatever address it
  had, so a cluster which moved follows along.
