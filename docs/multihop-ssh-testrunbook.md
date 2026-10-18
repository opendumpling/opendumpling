# Testing multi-hop workers and SSH certificate injection yourself

This is a runbook to walk through both new features end to end on a single
machine (macOS or Linux):

1. **Multi-hop workers** — a *private* (egress) worker that dials out to a
   *public* (ingress) worker, so the private worker needs no inbound port at
   all.
2. **SSH certificate injection** — the worker terminates the client's SSH
   connection and establishes a new SSH connection to the endpoint using an
   injected short lived certificate. The private key never reaches the client.

In this setup the private worker acts as the egress worker: it is the only one
that can reach the endpoint, and the public worker acts as the ingress.

```
 client ──ssh──▶ public worker ──tunnel──▶ private worker ──tcp:22──▶ linux target
   (ingress)         (ingress)                                              (egress)
```

---

## 0. Prerequisites

- Go 1.20+ (for `make` / `go build`)
- PostgreSQL (or Docker)
- Vault (only needed for the SSH CA; see step 6)
- A Linux host running sshd to use as the target

Build the binary:

```sh
make dev   # or: go build -o bin/dumpling ./cmd/dumpling
export PATH="$PWD/bin:$PATH"
```

Create a working directory:

```sh
export DEMO=~/dumpling-multihop-demo
mkdir -p "$DEMO"
cd "$DEMO"
```

---

## 1. Database

```sh
createdb dumpling      # or via docker:
# docker run -d --name dumpling-pg -e POSTGRES_PASSWORD=dumpling \
#   -e POSTGRES_USER=dumpling -p 5432:5432 postgres:15

export DB_URL="postgresql://dumpling:dumpling@127.0.0.1:5432/dumpling?sslmode=disable"
dumpling database init -url "$DB_URL"
```

---

## 2. Controller

`controller.hcl`:

```hcl
disable_mlock = true

controller {
  name = "demo-controller"
  database {
    url = "env://DB_URL"
  }
  public_cluster_addr = "127.0.0.1:9201"
}

listener "tcp" {
  address     = "127.0.0.1"
  purpose     = "api"
  tls_disable = true
}

listener "tcp" {
  address = "127.0.0.1"
  purpose = "cluster"
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
```

Start the controller:

```sh
dumpling server -config controller.hcl
```

In a second terminal:

```sh
export BOUNDARY_ADDR="http://127.0.0.1:9200"
dumpling authenticate -auth-method-id <ampw_...> -name admin
```

The auth method id is in the output of `database init` (the dev password auth
method). The password of the `admin` user from the dev install is `password`.
Save the token:

```sh
export BOUNDARY_TOKEN=<token from the authenticate output>
```

---

## 3. Public worker (ingress)

This worker is reachable from outside and accepts the client connections. It
needs an *ingress worker filter* so the controller knows it is the entrance;
see step 8.

`public-worker.hcl`:

```hcl
worker {
  name              = "public"
  description       = "Public (ingress) worker"
  public_addr       = "127.0.0.1:9202"
  initial_upstreams = ["127.0.0.1:9201"]
}

listener "tcp" {
  address = "127.0.0.1"
  purpose = "proxy"
}

listener "tcp" {
  address = "127.0.0.1"
  purpose = "cluster"
}

kms "aead" {
  purpose   = "worker-auth"
  aead_type = "aes-gcm"
  key       = "8fZBjCUfN0TzjEGLQldGY4+iE9AkOvCfjh7+p0GtRBQ="
  key_id    = "global_worker-auth"
}
```

> The `worker-auth` key must match the one in the controller config exactly.

```sh
dumpling server -config public-worker.hcl
```

---

## 4. Private worker (egress)

This worker sits behind the public worker and **dials out** to port 9203 of
the public worker. It needs no reachable inbound port at all; the `proxy`
listener here deliberately binds to `127.0.0.1` so only local processes could
ever reach it.

`private-worker.hcl`:

```hcl
worker {
  name              = "private"
  description       = "Private (egress) worker"
  initial_upstreams = ["127.0.0.1:9203"]

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
  address = "127.0.0.1"
  purpose = "cluster"
}

kms "aead" {
  purpose   = "worker-auth"
  aead_type = "aes-gcm"
  key       = "8fZBjCUfN0TzjEGLQldGY4+iE9AkOvCfjh7+p0GtRBQ="
  key_id    = "global_worker-auth"
}
```

> Important: `initial_upstreams` points at the **public worker's cluster
> listener** (9203), not at the controller. The private worker registers with
> the controller through the public worker (nodeenrollment multi-hop chain)
> and additionally keeps a permanent pool of data plane connections open to
> that same public worker.

```sh
dumpling server -config private-worker.hcl
```

### Verification

```sh
dumpling workers list -format json
```

Both workers should show up. On the public worker you should see
`downstream_worker_count: 1`; on the private worker `0`.

In the public worker's log, after a few seconds, you should see something
like:

```
accepted downstream data plane connection  worker_id=wprv_...
```

That is the pool of data plane connections the private worker has set up.
Without that pool no multi hop session can run.

---

## 5. Preparing the target (Linux)

The private worker must be the only one that can reach the target. In a
realistic setup the target lives in the same network as the private worker;
below I use `127.0.0.1:22` because everything runs on one machine. Change
`<TARGET_IP>` to whatever host you actually want to use.

sshd must trust the Dumpling CA so a signed certificate is accepted:

```sh
# on the target
sudo tee /etc/ssh/trusted_user_ca_keys.d/dumpling.pub >/dev/null <<'EOF'
<public key from step 6>
EOF
echo "TrustedUserCAKeys /etc/ssh/trusted_user_ca_keys.d/dumpling.pub" | \
  sudo tee /etc/ssh/sshd_config.d/99-dumpling.conf
sudo systemctl restart sshd     # or: sudo service ssh restart
```

---

## 6. SSH CA in Vault

The SSH certificates come from the `vault-ssh-certificate` credential library.
Set up a CA:

```sh
export VAULT_ADDR="http://127.0.0.1:8200"
export VAULT_TOKEN=<root token>

vault secrets enable -path=ssh-ca ssh

# Generate the signing key and export the public part (needed for step 5)
vault write -format=json ssh-ca/config/ca \
  generate_signing_key=true | jq -r .data.public_key

# A role that allows signing
vault write ssh-ca/roles/dumpling-target - <<EOF
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

Give Dumpling access to that one sign route:

```sh
vault policy write dumpling-ssh - <<'EOF'
path "ssh-ca/sign/dumpling-target" {
  capabilities = ["update"]
}
EOF

vault token create -policy=dumpling-ssh -ttl=8h
```

---

## 7. Creating the resources

Use `-scope-global` for everything so you do not have to set up roles and
grants.

```sh
# Host catalog + host
HC=$(dumpling host-catalogs create static -scope-global -format json | jq -r .item.id)
HS=$(dumpling host-sets create static -host-catalog-id "$HC" -format json | jq -r .item.id)
dumpling hosts create static -host-catalog-id "$HC" -address <TARGET_IP>

# Credential store + SSH cert library
CS=$(dumpling credential-stores create vault -scope-global \
       -address "$VAULT_ADDR" -token "$VAULT_TOKEN" -format json | jq -r .item.id)
CL=$(dumpling credential-libraries create vault-ssh-certificate \
       -credential-store-id "$CS" \
       -vault-path "ssh-ca/sign/dumpling-target" \
       -username root -format json | jq -r .item.id)

# SSH target
T=$(dumpling targets create ssh -scope-global -default-port 22 -name linux -format json | jq -r .item.id)
dumpling targets add-host-sets -id "$T" -host-set "$HS"
dumpling targets set-credential-sources -id "$T" \
  -injected-application-credential-source "$CL"
```

> Note: the credential purpose must be `injected_application`. Only then does
> the controller send the certificate along in the `SshProtocolContext` and
> the worker injects it. With `-brokered-credential-source` you get the old
> behaviour (the client sees the credentials) and SSH injection does not work.

---

## 8. Route filters on the target

This is the switch that forces multi-hop. Without filters the controller
picks the shortest route: the public worker would try to dial the endpoint
itself.

```sh
dumpling targets update -id "$T" \
  -ingress-worker-filter '"/name" == "public"' \
  -egress-worker-filter  '"/name" == "private"'
```

What happens now at `authorize-session`:

- the controller picks the worker matching the **ingress** filter (public)
  and returns it to the client;
- at `authorize-connection` the controller computes the route from that
  ingress worker to a worker matching the **egress** filter (private), using
  the in-memory downstream graph that is kept up to date live from the
  workers' status updates;
- the route `[public, private]` is returned in `AuthorizeConnectionResponse`
  and the public worker tunnels the session through the private worker.

Check that the filters are set:

```sh
dumpling targets read -id "$T" | grep -i worker-filter
```

---

## 9. Connecting

```sh
dumpling connect -target-id "$T" -format json
```

You get back the local proxy address, for example:

```json
{ "address": "127.0.0.1", "port": 51234, "protocol": "tcp", "session_id": "s1234567890" }
```

Open an SSH connection to it:

```sh
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
    -p 51234 root@127.0.0.1
```

You should now be logged in. On the target host, `/var/log/auth.log` (or
`journalctl -u ssh`) shows a login with a *certificate*, not a password and
not an authorized key:

```
Accepted publickey for root from ... port ... ssh2: ED25519-CERT ID ...
```

### What to look for to be sure multi-hop is used

| Where | What |
| --- | --- |
| `dumpling sessions read -id <s...>` | `status: active`, `connection_limit` |
| public worker log | `accepted downstream data plane connection worker_id=wprv_...` |
| public worker log | no connection to `<TARGET_IP>:22` (only the private worker dials the target) |
| private worker log | `data plane connection to upstream ended` after the session closed (connections are single use and get replaced) |
| `dumpling workers read -id <public>` | `downstream_worker_count: 1` |
| ports on the target | only port 22 is open; the public worker has *no* route to the target |

The easiest check: bind the target to an interface the public worker cannot
reach (or firewall `<TARGET_IP>:22` from the public worker). If `ssh` still
works, the session really went through the private worker.

---

## 10. Variations

### Three hops

Add a second private worker whose `initial_upstreams` points at the *first*
private worker, and move the egress filter to the new worker:

```
public ──▶ private-a ──▶ private-b ──▶ target
```

The route becomes `[public, private-a, private-b]`. The intermediate hop
(forwards the dial request to the next hop in the route) and the leaf hop
dials the endpoint. The pool size applies per hop.

### TCP target over multi-hop

Works identically; just create a `tcp` target with the same filters instead of
an `ssh` target. Handy for ruling SSH out as a source of problems.

### Without a Vault SSH CA

If you only want to test multi-hop without Vault, create a `tcp` target with
`-brokered-credential-source` (or no credentials) and connect with
`dumpling connect` plus any client to the published port number.

---

## 11. Constraints and known limitations

- **`data_plane_pool_size`** (default 10) determines how many *concurrent*
  multi-hop sessions a hop can carry; every data plane connection carries
  exactly one session and is then closed and replaced. When the pool runs
  dry, a new session waits at most 30 seconds.
- **Endpoint host key verification**: the worker does not currently verify
  the SSH endpoint's host key (`ssh.InsecureIgnoreHostKey`). This is an
  explicit TODO and means a MITM between the private worker and the target
  goes unnoticed. Fix this before putting this into real use.
- **The private worker still needs a proxy listener** because the worker
  bootstrap requires one; bind it to `127.0.0.1`. It is not a security
  problem — clients are never sent to that worker, only session data flows
  through it — but it is not a clean "no inbound ports" configuration either.
- **The downstream graph is eventually consistent.** The controller learns
  the public→private relation from the status updates. Right after the
  private worker starts, an `authorize-session` may still fail with "no route
  to an egress worker matching worker filter"; waiting a moment and retrying
  resolves it.
- **Worker tags** are an alternative to the name filters:
  `-ingress-worker-filter '"/tags/region" == "eu"'`.

---

## 12. Cleanup

```sh
dumpling targets delete -id "$T"
dumpling credential-libraries delete -id "$CL"
dumpling credential-stores delete -id "$CS"
dumpling host-sets delete -id "$HS"
dumpling host-catalogs delete -id "$HC"
dropdb dumpling
rm -rf "$DEMO"
```
