# lxd_auth_group

Provides information about an existing LXD authorization group.

## Example Usage

```hcl
data "lxd_auth_group" "group" {
  name = "admins"
}
```

## Argument Reference

* `name` - **Required** - Name of the group.

* `remote` - *Optional* - The remote in which the resource will be created. If
	not provided, the provider's default remote will be used.

## Attribute Reference

This data source exports the following attributes in addition to the arguments above:

* `description` - Description of the group.

* `permissions` - List of group permissions.
