package server_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/terraform-lxd/terraform-provider-lxd/internal/acctest"
)

func TestAccServer_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.PreCheck(t)
			acctest.PreCheckServerTests(t)
			acctest.PreCheckAPIExtensions(t, "metadata_configuration")
		},
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Provider() + testAccServer_config(`
    "images.auto_update_interval" = "15"
`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_server.global", "config.images.auto_update_interval", "15"),
				),
			},
		},
	})
}

func TestAccServer_update(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.PreCheck(t)
			acctest.PreCheckServerTests(t)
			acctest.PreCheckAPIExtensions(t, "metadata_configuration")
		},
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Provider() + testAccServer_config(`
    "images.auto_update_interval" = "15"
`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_server.global", "config.images.auto_update_interval", "15"),
				),
			},
			{
				Config: acctest.Provider() + testAccServer_config(`
    "images.auto_update_interval"  = "30"
    "images.compression_algorithm" = "gzip"
`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_server.global", "config.images.auto_update_interval", "30"),
					resource.TestCheckResourceAttr("lxd_server.global", "config.images.compression_algorithm", "gzip"),
				),
			},
			{
				Config: acctest.Provider() + testAccServer_config(""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_server.global", "config.%", "0"),
				),
			},
		},
	})
}

func TestAccServer_localKeyRejected(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.PreCheck(t)
			acctest.PreCheckServerTests(t)
			acctest.PreCheckAPIExtensions(t, "metadata_configuration")
		},
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Provider() + testAccServer_config(`
    "core.https_address" = ":8443"
`),
				ExpectError: regexp.MustCompile("member-specific configuration key"),
			},
		},
	})
}

func testAccServer_config(config string) string {
	return fmt.Sprintf(`
resource "lxd_server" "global" {
  config = {
%s  }
}`, config)
}
