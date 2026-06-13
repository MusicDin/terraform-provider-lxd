# lxd_server

Manages global LXD server configuration.

This resource requires the LXD server to support the `metadata_configuration`
API extension.

Only the configuration keys explicitly set in `config` are managed by this
resource. Any other server configuration is left untouched, both when the
resource is created and when it is destroyed. Removing a key from `config`
resets it back to its default value.

Member-specific (per cluster member) configuration keys are not supported and
are rejected.

## Example Usage

```hcl
resource "lxd_server" "global" {
  config = {
    "images.auto_update_interval" = "15"
    "core.https_allowed_origin"   = "*"
  }
}
```

## Argument Reference

* `config` - *Optional* - Map of key/value pairs of
	[global server config settings](https://documentation.ubuntu.com/lxd/latest/reference/server_settings/).

* `remote` - *Optional* - The remote in which the resource will be configured.
	If not provided, the provider's default remote will be used.

## Attribute Reference

No attributes are exported.

## Import

LXD servers can be imported by specifying the remote name, or an empty string
to import the default remote:

```shell
terraform import lxd_server.global ""
```
