package server_test

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
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
`, nil),
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
`, nil),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_server.global", "config.images.auto_update_interval", "15"),
				),
			},
			{
				Config: acctest.Provider() + testAccServer_config(`
    "images.auto_update_interval"  = "30"
    "images.compression_algorithm" = "gzip"
`, nil),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_server.global", "config.images.auto_update_interval", "30"),
					resource.TestCheckResourceAttr("lxd_server.global", "config.images.compression_algorithm", "gzip"),
				),
			},
			{
				Config: acctest.Provider() + testAccServer_config("", nil),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_server.global", "config.%", "0"),
				),
			},
		},
	})
}

func TestAccServer_localKey(t *testing.T) {
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
    "core.bgp_routerid" = "127.0.0.1"
`, nil),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_server.global", "config.core.bgp_routerid", "127.0.0.1"),
					resource.TestCheckResourceAttr("lxd_server.global", "members.%", "0"),
				),
			},
			{
				Config: acctest.Provider() + testAccServer_config("", nil),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_server.global", "config.%", "0"),
				),
			},
		},
	})
}

func TestAccServer_invalidKeyRejected(t *testing.T) {
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
    "not.a.real.key" = "value"
`, nil),
				ExpectError: regexp.MustCompile("is not a valid server configuration key"),
			},
		},
	})
}

func TestAccServer_memberOverridesRequiresCluster(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.PreCheck(t)
			acctest.PreCheckServerTests(t)
			acctest.PreCheckStandalone(t)
			acctest.PreCheckAPIExtensions(t, "metadata_configuration")
		},
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Provider() + testAccServer_config("", map[string]map[string]string{
					"node1": {"core.bgp_routerid": "127.0.0.1"},
				}),
				ExpectError: regexp.MustCompile("allowed only when LXD is clustered"),
			},
		},
	})
}

func TestAccServer_clusterMemberOverrides(t *testing.T) {
	targets := acctest.PreCheckClustering(t, 2)

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
    "core.bgp_routerid" = "127.0.0.1"
`, map[string]map[string]string{
					targets[0]: {"core.bgp_routerid": "127.0.0.2"},
				}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_server.global", "config.core.bgp_routerid", "127.0.0.1"),
					resource.TestCheckResourceAttr("lxd_server.global", "members.%", fmt.Sprintf("%d", len(targets))),
					resource.TestCheckResourceAttr("lxd_server.global", fmt.Sprintf("members.%s.config.core.bgp_routerid", targets[0]), "127.0.0.2"),
					resource.TestCheckResourceAttr("lxd_server.global", fmt.Sprintf("members.%s.config.core.bgp_routerid", targets[1]), "127.0.0.1"),
				),
			},
			{
				Config: acctest.Provider() + testAccServer_config("", nil),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_server.global", "config.%", "0"),
					resource.TestCheckResourceAttr("lxd_server.global", "member_overrides.%", "0"),
				),
			},
		},
	})
}

func testAccServer_config(config string, memberOverrides map[string]map[string]string) string {
	var b strings.Builder

	b.WriteString(`
resource "lxd_server" "global" {
  config = {
`)
	b.WriteString(config)
	b.WriteString("  }\n")

	if len(memberOverrides) > 0 {
		memberNames := make([]string, 0, len(memberOverrides))
		for member := range memberOverrides {
			memberNames = append(memberNames, member)
		}

		sort.Strings(memberNames)

		b.WriteString("\n  member_overrides = {\n")
		for _, member := range memberNames {
			overrideConfig := memberOverrides[member]

			fmt.Fprintf(&b, "    %q = {\n", member)
			b.WriteString("      config = {\n")

			overrideKeys := make([]string, 0, len(overrideConfig))
			for key := range overrideConfig {
				overrideKeys = append(overrideKeys, key)
			}

			sort.Strings(overrideKeys)
			for _, key := range overrideKeys {
				fmt.Fprintf(&b, "        %q = %q\n", key, overrideConfig[key])
			}

			b.WriteString("      }\n")
			b.WriteString("    }\n")
		}

		b.WriteString("  }\n")
	}

	b.WriteString("}")

	return b.String()
}
