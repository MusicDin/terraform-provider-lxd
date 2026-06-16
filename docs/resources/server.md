# lxd_server

Manages LXD server configuration.

This resource requires the LXD server to support the `metadata_configuration`
API extension.

Only the configuration keys explicitly set in `config` (and, for clustered
servers, `member_overrides`) are managed by this resource. Any other server
configuration is left untouched, both when the resource is created and when it
is destroyed. Removing a key resets it back to its default value.

`config` may contain both global (cluster-wide) configuration keys and
member-specific (local) configuration keys. Local keys act as defaults applied
to every cluster member, unless overridden for a specific member via
`member_overrides`. On a non-clustered server, local keys apply directly to the
single server and `member_overrides` is not allowed.

The `members` attribute is computed and reflects the resolved, per-member
value of every member-specific key that is tracked via `config` or
`member_overrides`.

## Example Usage

### Global configuration

```hcl
resource "lxd_server" "global" {
  config = {
    "images.auto_update_interval" = "15"
    "core.https_allowed_origin"   = "*"
  }
}
```

### Clustered server with member-specific overrides

```hcl
resource "lxd_server" "global" {
  config = {
    "core.bgp_routerid" = "127.0.0.1"
  }

  member_overrides = {
    "node1" = {
      config = {
        "core.bgp_routerid" = "127.0.0.2"
      }
    }
  }
}
```

## Argument Reference

* `config` - *Optional* - Map of key/value pairs of
	[server config settings](https://documentation.ubuntu.com/lxd/latest/reference/server_settings/).
	May contain both global and member-specific (local) configuration keys.
	Local keys are applied as defaults to all cluster members.

* `member_overrides` - *Optional* - Map (member name to config) of
	member-specific configuration overrides. Only member-specific
	(local-scope) configuration keys are allowed. Overrides take precedence
	over the local defaults defined in `config`. Allowed only when LXD is
	clustered.

* `remote` - *Optional* - The remote in which the resource will be configured.
	If not provided, the provider's default remote will be used.

## Attribute Reference

* `members` - Map (member name to config) of the resolved member-specific
	configuration, limited to the keys tracked via `config` and
	`member_overrides`.

## Import

LXD servers can be imported by specifying the remote name, or an empty string
to import the default remote:

```shell
terraform import lxd_server.global ""
```
