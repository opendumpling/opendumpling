# SSH aliases and ProxyCommand

OpenDumpling aliases give an SSH target a human-readable value. An alias is
unique within its scope, can be created in a global, organization, or project
scope, and points to a target. The SSH `ProxyCommand` integration uses that
alias to authorize a fresh session each time SSH connects; it does not cache a
session or run a background agent.

## Create an alias

Create an alias in a scope where you have permission to create aliases. The
destination must be a target that accepts SSH:

```sh
dumpling aliases create \
  -scope-id p_1234567890 \
  -name production-database \
  -value db.example \
  -destination-id ttcp_1234567890
```

Alias values may contain letters, digits, dots, dashes, and underscores. The
same value can be used in different scopes; it cannot be repeated within one
scope. The caller must have permission to read the alias and authorize a
session against its destination target. Authorization is checked on every
connection.

## Generate SSH configuration

Log in to the controller with the Dumpling CLI, then generate configuration
for aliases visible from the global scope and its descendants:

```sh
dumpling ssh-config >> ~/.ssh/config
chmod 600 ~/.ssh/config
```

The generated entries pin each alias to its scope. If the same alias value is
present in multiple scopes, the generated SSH host names get a scope suffix so
each entry remains selectable. You can select a different starting scope with
`-scope-id` and control child-scope traversal with `-recursive`.

Connect with the generated host name:

```sh
ssh db.example
```

SSH invokes a command equivalent to:

```sshconfig
Host db.example
  ProxyCommand dumpling connect -format ssh-proxy -alias db.example -alias-scope-id p_1234567890
```

The `ssh-proxy` format uses stdin and stdout exclusively for the SSH byte
stream. Diagnostics are written to stderr. To configure an entry manually,
use the same ProxyCommand and provide `-alias-scope-id` whenever the value may
exist in more than one scope. Without a scope ID, the CLI searches recursively
from the global scope and asks for disambiguation if it finds multiple matches.

The local SSH client still handles SSH host-key checking and user
authentication. Configure those as usual in `~/.ssh/config` (for example with
`User`, `IdentityFile`, and `HostKeyAlias`). Dumpling transports the raw SSH
connection to the target; it does not configure the SSH client's identity or
host-key policy.
