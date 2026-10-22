# Zero-trust SSH in practice: multi-hop workers and credential injection with OpenDumpling

*How to reach a machine that has no inbound ports, using a credential that never touches the client — a hands-on walkthrough you can run on your laptop with nothing but Docker.*

---

Most SSH setups quietly rely on two brittle assumptions. The first is that your laptop holds a private key that opens a door on the server. The second is that every hop between you and that server can be reached directly. Both assumptions are exactly what make SSH hard to run at scale: keys sprawl onto machines, and the moment a host sits behind NAT or a firewall, "just SSH in" stops working.

OpenDumpling — the MPL-2.0, community-run fork of HashiCorp Boundary — attacks both at once. In this walkthrough we run its two most interesting behaviours end to end:

1. **Multi-hop workers.** A *private* (egress) worker dials *out* to a *public* (ingress) worker. The private one needs no inbound port at all.
2. **Credential injection.** The worker terminates your SSH connection and opens a *new* one to the target using a credential it received from the controller. Your client never holds the key.

We will do it all in containers on a single machine. Nothing is simulated: the private worker and the target share a network the public worker is *not* on, so if `ssh` still works, the session genuinely travelled the long way round.

```text
 client ──ssh──▶ public worker ──tunnel──▶ private worker ──tcp:22──▶ ssh target
  (you)            (ingress)                 (egress)
```

Everything below is plain Docker. It works identically with Podman; where the two differ is noted at the end. No cloud account, no Kubernetes, no managed control plane.

---

## What you need

- A container runtime: **Docker** (primary below) or **Podman**.
- The OpenDumpling repository checked out locally.
- `jq` on your machine.
- A Go toolchain only if you want to build the CLI the easy way.

Build the image once, from the repository root:

```sh
cd /path/to/opendumpling
docker build --build-arg VERSION=0.0.0-dev -t dumpling:dev .
```

And build the CLI you will drive the API with:

```sh
make build
export PATH="$PWD/bin:$PATH"
dumpling version
```

Create a scratch directory and move on.

```sh
export DEMO=/tmp/dumpling-demo
mkdir -p "$DEMO"
cd "$DEMO"
```

> **Podman note:** the commands below are Docker's. With Podman, substitute `podman` for `docker`; `docker network create`, `docker run`, `docker exec`, `docker logs` all have direct equivalents. Build with the same version argument: `podman build --build-arg VERSION=0.0.0-dev -t dumpling:dev .`.

---

## Why you don't need a secrets manager for this

There is a reason this walkthrough fits in a coffee break: OpenDumpling's SSH proxy is **credential agnostic**.

The worker code that fronts SSH — `internal/daemon/worker/proxy/ssh/ssh.go` — takes whatever credential the controller handed it and turns it into an SSH authentication method for the *endpoint*:

- an OpenBao-signed certificate → a certificate signer,
- a plain SSH private key → a key signer,
- a username and password → password auth.

The controller builds that equally from OpenBao-backed libraries and from *static* credentials. A static `ssh_private_key` is converted to the same `Credential_SshPrivateKey` message that a signed certificate becomes (`internal/daemon/controller/handlers/targets/credentials.go`). The worker path is identical either way.

So the quickest way to see injection work is a static credential store. We will bolt on the OpenBao certificate variant at the end for completeness. What matters is not where the key came from, but that **the client never sees it** — and that is true in both cases.

---

## 1. Two networks

We create two isolated networks. The public worker lives on `net-front`; the private worker and the target live on `net-back`. The private worker bridges both, so it can reach the public worker *and* the target. The public worker cannot reach the target at all.

```sh
docker network create net-front
docker network create net-back
```

```text
             net-front                          net-back
  controller ── public ──┐              private ── target
       │                 └──────────────────┘
       └───────────────────────────────────────────┘
                 (controller reaches private too)
```

That gap is the whole point. When you disconnect the private worker later and watch it fail, you will have proven the route.

The HCL addresses `controller`, `public`, and `private` are Docker network
aliases; the containers themselves are named `dm-controller`, `dm-public`, and
`dm-private`. The run commands below attach the aliases on the networks where
those names need to resolve. The target uses its container name, `dm-target`,
directly.

---

## 2. OpenBao and database

The controller stores its state in Postgres. OpenBao Transit is used as the
controller's **root KMS**; it is unrelated to the AWS host plugin (which is a
separate host-discovery feature). This demo uses OpenBao's dev mode and root
token, so do not reuse these settings in production.

```sh
docker run -d --name dm-openbao \
  --network net-front \
  -p 8200:8200 \
  openbao/openbao:2 server -dev -dev-root-token-id=root \
    -dev-listen-address=0.0.0.0:8200

until docker exec -e BAO_ADDR=http://127.0.0.1:8200 dm-openbao bao status >/dev/null 2>&1; do sleep 1; done

docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e BAO_TOKEN=root dm-openbao \
  bao secrets enable -path=transit transit
docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e BAO_TOKEN=root dm-openbao \
  bao write -f transit/keys/dumpling-root

docker run -d --name dm-pg \
  --network net-front \
  -e POSTGRES_USER=dumpling \
  -e POSTGRES_PASSWORD=dumpling \
  -e POSTGRES_DB=dumpling \
  postgres:15

until docker exec dm-pg pg_isready -U dumpling >/dev/null 2>&1; do sleep 1; done
```

Create `$DEMO/controller.hcl` **before** initializing the database. The
database URL is intentionally literal here: both `database init` and the
controller need it, and the migration URL is supplied explicitly to init.

```hcl
disable_mlock = false

cluster_id = "c-demo"

controller {
  name = "demo-controller"
  database {
    url = "postgresql://dumpling:dumpling@dm-pg:5432/dumpling?sslmode=disable"
  }
  public_cluster_addr = "controller:9201"
}

listener "tcp" {
  address     = "0.0.0.0"
  purpose     = "api"
  tls_disable = true
}

listener "tcp" {
  address = "0.0.0.0"
  purpose = "cluster"
}

# Health checks are served by the ops listener, not the API listener.
listener "tcp" {
  address     = "0.0.0.0"
  purpose     = "ops"
  tls_disable = true
}

# The transit plugin uses OpenBao's API. The root token is only for this
# throwaway dev-mode demo; use a narrowly scoped token outside the demo.
kms "transit" {
  purpose         = "root"
  address         = "http://dm-openbao:8200"
  token           = "root"
  mount_path      = "transit"
  key_name        = "dumpling-root"
  disable_renewal = "true"
}

# Shared worker-auth key for this local demo. Workers must use the same key.
kms "aead" {
  purpose   = "worker-auth"
  aead_type = "aes-gcm"
  key       = "8fZBjCUfN0TzjEGLQldGY4+iE9AkOvCfjh7+p0GtRBQ="
  key_id    = "global_worker-auth"
}
```

Initialize the schema using the full CLI command (the image entrypoint does not
reliably dispatch the bare `database` subcommand). `IPC_LOCK` is needed because
mlock is enabled in the config:

```sh
docker run --rm --network net-front --cap-add IPC_LOCK \
  -v "$DEMO/controller.hcl:/dumpling/config.hcl:ro" \
  dumpling:dev \
  dumpling database init \
  -config /dumpling/config.hcl \
  -migration-url "postgresql://dumpling:dumpling@dm-pg:5432/dumpling?sslmode=disable"
```

The output includes a dev password auth method id (`ampw_...`), the login name
`admin`, and a password. Keep the auth method id handy for step 4.

---

## 3. Controller

The controller configuration was created in step 2, before database init.
Start it, publishing the API port to your machine:

```sh
docker run -d --name dm-controller \
  --network net-front \
  --network-alias controller \
  --cap-add IPC_LOCK \
  -p 9200:9200 -p 9203:9203 \
  -v "$DEMO/controller.hcl:/dumpling/config.hcl:ro" \
  dumpling:dev server -config /dumpling/config.hcl

until curl -sf http://127.0.0.1:9203/health >/dev/null; do sleep 1; done
```

The controller API is reachable from your shell at `127.0.0.1:9200`; the ops
health endpoint is at `127.0.0.1:9203/health`. Other containers reach the API
as `controller:9200` and the cluster listener as `controller:9201`.
The root KMS is OpenBao Transit; the AWS host plugin loaded by the controller is
not a KMS and is not involved in encrypting Dumpling's data.

---

## 4. Authenticate

```sh
export DUMPLING_ADDR="http://127.0.0.1:9200"

dumpling authenticate password \
  -auth-method-id <ampw_... from step 2> \
  -login-name admin \
  -password password

export DUMPLING_TOKEN=<token from the output>
```

---

## 5. Public worker (ingress)

This is the worker your client connects to. It carries a `worker-auth` KMS that matches the controller's, so it authenticates with the shared key and needs no registration. It is also the kind of worker that can accept *downstream* workers — the private one.

`public-worker.hcl`:

```hcl
worker {
  name        = "public"
  description = "Public (ingress) worker"
  public_addr = "public:9202"
}

# The controller's cluster address. A worker with the shared key needs no
# discovery; it just needs to know whom to talk to.
initial_upstreams = ["controller:9201"]

listener "tcp" {
  address = "0.0.0.0"
  purpose = "proxy"
}

listener "tcp" {
  address = "0.0.0.0"
  purpose = "cluster"
}

kms "aead" {
  purpose   = "worker-auth"
  aead_type = "aes-gcm"
  key       = "8fZBjCUfN0TzjEGLQldGY4+iE9AkOvCfjh7+p0GtRBQ="
  key_id    = "global_worker-auth"
}
```

```sh
docker run -d --name dm-public \
  --network net-front \
  --network-alias public \
  --cap-add IPC_LOCK \
  -v "$DEMO/public-worker.hcl:/dumpling/config.hcl:ro" \
  dumpling:dev server -config /dumpling/config.hcl
```

---

## 6. Private worker (egress)

This worker dials *out* to the public worker and has no reachable inbound port of its own. It joins both networks so it can reach the target on `net-back` and the public worker on `net-front`.

`private-worker.hcl`:

```hcl
worker {
  name        = "private"
  description = "Private (egress) worker"
  public_addr = "private:9202"

  # Register with the controller *through* the public worker: this is the
  # public worker's cluster listener, not the controller's.
  initial_upstreams = ["public:9201"]

  # Turn on the egress behaviour and keep a pool of pre-established data plane
  # connections towards the upstream(s).
  private {
    enabled              = true
    data_plane_pool_size = 10
  }
}

listener "tcp" {
  address = "127.0.0.1"
  purpose = "proxy"
}

listener "tcp" {
  address = "0.0.0.0"
  purpose = "cluster"
}

kms "aead" {
  purpose   = "worker-auth"
  aead_type = "aes-gcm"
  key       = "8fZBjCUfN0TzjEGLQldGY4+iE9AkOvCfjh7+p0GtRBQ="
  key_id    = "global_worker-auth"
}
```

Two things to notice:

- `initial_upstreams` points at the **public worker's cluster listener** (`public:9201`). The private worker registers with the controller through the public worker, and on top of that keeps a permanent pool of data-plane connections to it (`internal/daemon/worker/dataplane_upstream.go`). Those pre-established connections are what let a session flow *back* to a worker nobody can dial.
- The `proxy` listener still binds `127.0.0.1` inside the container. The worker bootstrap requires one; nothing outside can reach it.

```sh
docker run -d --name dm-private \
  --network net-front \
  --network-alias private \
  --cap-add IPC_LOCK \
  -v "$DEMO/private-worker.hcl:/dumpling/config.hcl:ro" \
  dumpling:dev server -config /dumpling/config.hcl

docker network connect --alias private net-back dm-private
```

### Check the pool came up

```sh
docker logs dm-public 2>&1 | grep -i downstream | head
```

You should see a series of lines like:

```text
accepted downstream data plane connection  worker_id=wprv_...
```

That is the pool of ten connections the private worker opened. No pool, no multi-hop session. You can also read the workers:

```sh
dumpling workers list -format json | jq -r '.items[].id' | while read id; do
  dumpling workers read -id "$id" -format json \
    | jq '{name: .item.name, downstream: .item.downstream_worker_count}'
done
```

The public worker reports `downstream_worker_count: 1`; the private worker `0`.

---

## 7. An SSH target only the private worker can see

The target must be reachable **only** from the private worker. We build a tiny sshd container, put it on `net-back` alone, and give it an authorized key.

`target.Dockerfile`:

```dockerfile
FROM debian:12-slim
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      openssh-server bash \
 && rm -rf /var/lib/apt/lists/* \
 && mkdir -p /run/sshd /etc/ssh/trusted_user_ca_keys.d
COPY bootstrap.sh /usr/local/bin/bootstrap.sh
RUN chmod +x /usr/local/bin/bootstrap.sh
EXPOSE 22
CMD ["/usr/local/bin/bootstrap.sh"]
```

`target/bootstrap.sh`:

```sh
#!/bin/bash
set -e

# Authorized key comes from the mounted directory.
install -d -m 700 -o root -g root /root/.ssh
cat /keys/authorized_key.pub > /root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys

# Trust a CA if one was mounted (used later for the certificate variant).
CA=/etc/ssh/trusted_user_ca_keys.d/dumpling.pub
if [ -s "$CA" ]; then
  echo "TrustedUserCAKeys $CA" > /etc/ssh/sshd_config.d/99-dumpling.conf
fi

ssh-keygen -A
exec /usr/sbin/sshd -D -e
```

Generate a keypair — this is the credential that will be *injected*; your client will never see it:

```sh
mkdir -p "$DEMO/target" "$DEMO/keys"
ssh-keygen -t ed25519 -N "" -f "$DEMO/keys/target_key"
cp "$DEMO/keys/target_key.pub" "$DEMO/keys/authorized_key.pub"
: > "$DEMO/keys/dumpling.pub"    # no CA yet

cd "$DEMO"
docker build -t dm-target -f target.Dockerfile target
docker run -d --name dm-target --network net-back -v "$DEMO/keys:/keys:ro" dm-target
```

Prove the isolation: the private worker reaches the target; the public worker cannot even resolve it.

```sh
docker exec dm-private sh -c 'nc -vz dm-target 22 2>&1 | head -1 || true'
docker exec dm-public  sh -c 'nc -vz dm-target 22 2>&1 | head -1 || true; echo "(public failing here is expected)"'
```

---

## 8. Wire up the resources

We use `-scope-global` throughout so there is no role or grant setup.

```sh
# A host catalog, a host set, and a host. The address is the *container* name,
# because it is resolved by the private worker, which is on net-back.
HC=$(dumpling host-catalogs create static -scope-global -format json | jq -r .item.id)
HS=$(dumpling host-sets create static -host-catalog-id "$HC" -format json | jq -r .item.id)
dumpling hosts create static -host-catalog-id "$HC" -address dm-target

# A static credential store — no secrets manager required.
CS=$(dumpling credential-stores create static -scope-global -format json | jq -r .item.id)

# The credential to inject: the private key from step 7, as user root.
PRIV=$(cat "$DEMO/keys/target_key")
CRED=$(dumpling credentials create ssh-private-key \
        -credential-store-id "$CS" \
        -username root \
        -private-key "$PRIV" \
        -format json | jq -r .item.id)

# The SSH target.
T=$(dumpling targets create ssh -scope-global -name linux \
      -default-port 22 -format json | jq -r .item.id)
dumpling targets add-host-sets -id "$T" -host-set "$HS"

# Attach the credential as INJECTED, not brokered.
dumpling targets set-credential-sources -id "$T" \
  -injected-application-credential-source "$CRED"
```

The word **injected** is the crux:

- `-injected-application-credential-source` → the controller sends the credential to the **worker**; the worker uses it against the endpoint; the client never sees it.
- `-brokered-credential-source` → the controller returns the credential to the **client**, which then uses it. (For a private key the CLI writes a temp file and passes `-i` — see `internal/cmd/commands/connect/ssh.go`.) The worker just proxies raw TCP. That is the old world.

You can watch the difference in the CLI's own behaviour later.

---

## 9. Force the multi-hop route

Without filters the controller takes the shortest path and the public worker would try to dial the target itself — and fail, because it cannot see it. The filters are the switch that routes the session through the private worker.

```sh
dumpling targets update -id "$T" \
  -ingress-worker-filter '"/name" == "public"' \
  -egress-worker-filter  '"/name" == "private"'
```

Verify:

```sh
dumpling targets read -id "$T" | grep -i worker-filter
```

What happens at authorization time (`internal/daemon/worker/proxy/multihopdialer.go`):

1. `authorize-session` picks the worker matching the **ingress** filter (public) and hands it to the client.
2. `authorize-connection` computes the route from ingress to a worker matching the **egress** filter (private) using the live downstream graph.
3. The route `[public, private]` comes back in the response. The public worker grabs one of its pooled data-plane connections to the private worker and forwards a `DataPlaneDialRequest` with an empty remaining route — which tells the private worker "you dial the endpoint."

The filters match on the worker's **name**, which is why the configs set `name = "public"` and `name = "private"`.

---

## 10. Connect

```sh
dumpling connect -target-id "$T" -format json
```

For an SSH target the CLI starts a local listener and execs your `ssh` client. You land as `root` on `dm-target`. On the target, the login is by **public key** — and the key came from the worker, not from you:

```sh
docker logs dm-target 2>&1 | tail -n 20
```

Look for:

```text
Accepted publickey for root from 172.x.x.x port ...: ED25519 SHA256:...
```

That `172.x.x.x` is the **private worker**, not your laptop.

### See the injection difference yourself

Watch which command the CLI runs. With injection, the client gets no key file and no `-i`. Now flip it:

```sh
# brokered: the CLI writes a temp key and passes -i, because the client is
# being handed the credential
dumpling targets set-credential-sources -id "$T" -brokered-credential-source "$CRED"
dumpling connect -target-id "$T" -format json

# back to injected
dumpling targets set-credential-sources -id "$T" \
  -injected-application-credential-source "$CRED"
```

Both connect, but only one keeps the private key off your machine. That is the promise of injection in a single command.

### Prove the route

| Where | What to look for |
| --- | --- |
| `dumpling sessions read -id <s...>` | `status: active` |
| `docker logs dm-public` | `accepted downstream data plane connection worker_id=wprv_...` |
| `docker logs dm-public` | **no** reachable `dm-target:22` (it is on the other network) |
| `docker logs dm-private` | `data plane connection to upstream ended` after the session closes |
| client | the accepted key on the target came from the private worker's IP |

The blunt proof:

```sh
docker network disconnect net-back dm-private
# try to connect again — it now fails, because the egress worker can't reach
# the target
docker network connect net-back dm-private
```

---

## 11. Optional: signed certificates from OpenBao

To exercise the certificate library instead of a static key, add OpenBao.

Configure an SSH CA and a signing role using the same OpenBao container started
in step 2:

```sh
docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e BAO_TOKEN=root dm-openbao \
  bao secrets enable -path=ssh-ca ssh

docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e BAO_TOKEN=root dm-openbao \
  bao write -format=json ssh-ca/config/ca generate_signing_key=true \
  | jq -r .data.public_key > "$DEMO/keys/dumpling.pub"

docker exec -i -e BAO_ADDR=http://127.0.0.1:8200 -e BAO_TOKEN=root dm-openbao \
  bao write ssh-ca/roles/dumpling-target - <<'EOF'
{
  "key_type": "ca",
  "allow_user_certificates": true,
  "allowed_users": "*",
  "allowed_extensions": "*",
  "default_extensions": { "permit-pty": "" },
  "default_user": "root",
  "ttl": "5m"
}
EOF
```

Restart the target so it trusts the CA:

```sh
docker restart dm-target
```

Create a Vault-flavoured credential store and library. Because the controller also runs in a container, the OpenBao address must be the container name:

```sh
CSV=$(dumpling credential-stores create vault -scope-global \
        -address "http://dm-openbao:8200" \
        -token root -format json | jq -r .item.id)

CL=$(dumpling credential-libraries create vault-ssh-certificate \
       -credential-store-id "$CSV" \
       -vault-path "ssh-ca/sign/dumpling-target" \
       -username root -format json | jq -r .item.id)

dumpling targets set-credential-sources -id "$T" \
  -injected-application-credential-source "$CL"
```

Connect again. Now the target's log shows a certificate identity rather than a bare key:

```text
Accepted publickey for root from 172.x.x.x ...: ED25519-CERT ID ...
```

> The subtypes are named `vault` and `vault-ssh-certificate` because those are the stable API identifiers OpenDumpling reads — the thing behind them is OpenBao.

---

## 12. Where this gets interesting

Once the pattern is in place, a few things follow naturally.

**Add a hop.** Give a second private worker `initial_upstreams` pointing at the *first* private worker, and move the egress filter to it:

```text
public ──▶ private-a ──▶ private-b ──▶ target
```

The route becomes `[public, private-a, private-b]`. The middle hop forwards the dial request down the line; the last one dials. The pool size applies per hop.

**Skip SSH entirely.** Use a `tcp` target with the same two filters. Handy for ruling SSH out as a variable.

**Prove the tunnel alone.** A `tcp` target with no credentials and the same filters; a bare `nc dm-target 22` from the client shows the tunnel without touching injection.

---

## 13. Honest limitations

These are the things to know before you point this at anything valuable.

- **Endpoint host-key verification is off.** The worker dials the endpoint with `ssh.InsecureIgnoreHostKey` (an explicit `TODO` in `internal/daemon/worker/proxy/ssh/ssh.go`). A man-in-the-middle between the private worker and the target is not detected today.
- **The private worker still runs a `proxy` listener.** Bind it to `127.0.0.1`; clients are never routed to it, only session bytes flow through it. It is not a literal zero-inbound-ports configuration yet.
- **`data_plane_pool_size` (default 10) caps concurrency.** Each data-plane connection carries exactly one session, then is closed and replaced. When the pool is drained, a new session waits up to 30 seconds.
- **The downstream graph is eventually consistent.** Right after the private worker starts, `authorize-session` can fail with "no route to an egress worker matching worker filter." Wait a second and retry.
- **Worker tags work as filters too:** `-ingress-worker-filter '"/tags/region" == "eu"'`.
- **Cross-container addressing.** The host address is the container name `dm-target` because the private worker resolves it. If you run the target as a host process instead, use `host.docker.internal` or your host IP.

---

## 14. Cleanup

```sh
dumpling targets delete -id "$T"
dumpling credential-libraries delete -id "$CL" 2>/dev/null
dumpling credential-stores delete -id "$CS"  2>/dev/null
dumpling credential-stores delete -id "$CSV" 2>/dev/null
dumpling host-sets delete -id "$HS"
dumpling host-catalogs delete -id "$HC"

docker rm -f dm-target dm-private dm-public dm-controller dm-pg dm-openbao 2>/dev/null
docker network rm net-front net-back 2>/dev/null
rm -rf "$DEMO"
```

---

## Appendix: Podman, and the code you just exercised

**Podman.** Replace `docker` with `podman` throughout. Networks, runs, execs and logs are command-for-command equivalents; `podman build -t dumpling:dev .` works the same. On rootless Podman, published ports and `dnf`/`apt` inside builds behave a little differently, but nothing in this walkthrough depends on those.

**Where the magic lives.** Each behaviour maps to a small piece of the codebase:

| Behaviour | Where |
| --- | --- |
| SSH termination + injection into the endpoint | `internal/daemon/worker/proxy/ssh/ssh.go` |
| Route → tunneled connection | `internal/daemon/worker/proxy/multihopdialer.go` |
| Worker-to-worker data-plane protocol | `internal/daemon/cluster/dataplane.go` |
| Private worker's outbound pool | `internal/daemon/worker/dataplane_upstream.go` |
| Ingress worker's pool of downstream connections | `internal/daemon/worker/dataplane_pool.go` |
| Serving downstream workers / chained registration | `internal/daemon/worker/secondary_connections.go` |
| Static credential → worker credential | `internal/daemon/controller/handlers/targets/credentials.go` |
| Client-side brokered vs injected | `internal/cmd/commands/connect/ssh.go` |
| Worker discovery by cluster id | `internal/daemon/worker/discovery.go` |
| `private { }` and `cluster_id` config | `internal/cmd/config/config.go` |

**Running without containers.** Everything here maps onto plain binaries on one machine — the only difference is that a single host is on "both networks" at once, so you lose the network gap that makes the route self-evident. The container version exists precisely to keep that gap honest.
