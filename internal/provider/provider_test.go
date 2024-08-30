package provider_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/terraform-lxd/terraform-provider-lxd/internal/acctest"
)

func TestAccProvider_configDir(t *testing.T) {
	defer resetLXDRemoteEnvVars()

	configDir := t.TempDir()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Ensure config dir is configurable using Terraform configuration.
				Config: testAccProvider_configDir(configDir),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_noop.noop", "remote", "local"),
					resource.TestCheckResourceAttr("lxd_noop.noop", "project", "default"),
					resource.TestCheckResourceAttrSet("lxd_noop.noop", "server_version"),
					testCheckClientCert(configDir, true),
				),
			},
		},
	})
	resetLXDRemoteEnvVars()
}

func TestAccProvider_trustToken(t *testing.T) {
	defer resetLXDRemoteEnvVars()

	token := acctest.ConfigureTrustToken(t)
	configDir := t.TempDir()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.PreCheck(t)
			acctest.PreCheckLocalServerHTTPS(t)
		},
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Ensure authentication fails with incorrect token.
				Config:      testAccProvider_remoteServer(configDir, "", "invalid", true),
				ExpectError: regexp.MustCompile(`not authorized`),
			},
			{
				// Ensure authentication succeeds with correct token.
				Config: testAccProvider_remoteServer(configDir, "", token, true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_noop.noop", "remote", "tf-remote"),
					resource.TestCheckResourceAttr("lxd_noop.noop", "project", "default"),
					resource.TestCheckResourceAttrSet("lxd_noop.noop", "server_version"),
				),
			},
			{
				// Ensure authentication succeeds if token is provided
				// as environment variable.
				PreConfig: func() {
					configDir = t.TempDir()
					os.Setenv("LXD_REMOTE", "tf-remote-token-fqdn")
					os.Setenv("LXD_ADDR", "https://127.0.0.1:8443")
					os.Setenv("LXD_TOKEN", acctest.ConfigureTrustToken(t))
				},
				Config: testAccProvider_remoteServerEnv(configDir),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_noop.noop", "remote", "tf-remote-token-fqdn"),
					resource.TestCheckResourceAttr("lxd_noop.noop", "project", "default"),
					resource.TestCheckResourceAttrSet("lxd_noop.noop", "server_version"),
				),
			},
		},
	})
}

func TestAccProvider_trustPassword(t *testing.T) {
	defer resetLXDRemoteEnvVars()

	password := acctest.ConfigureTrustPassword(t)
	configDir := t.TempDir()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.PreCheck(t)
			acctest.PreCheckLocalServerHTTPS(t)
		},
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Ensure authentication fails with incorrect password.
				Config:      testAccProvider_remoteServer(configDir, "invalid", "", true),
				ExpectError: regexp.MustCompile(`not authorized`),
			},
			{
				// Ensure authentication succeeds with correct token.
				Config: testAccProvider_remoteServer(configDir, password, "", true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_noop.noop", "remote", "tf-remote"),
					resource.TestCheckResourceAttr("lxd_noop.noop", "project", "default"),
					resource.TestCheckResourceAttrSet("lxd_noop.noop", "server_version"),
				),
			},
			{
				// Ensure authentication succeeds if password is provided
				// as environment variable.
				PreConfig: func() {
					configDir = t.TempDir()
					os.Setenv("LXD_REMOTE", "tf-remote-pass-fqdn")
					os.Setenv("LXD_ADDR", "https://127.0.0.1:8443")
					os.Setenv("LXD_PASSWORD", acctest.ConfigureTrustPassword(t))
				},
				Config: testAccProvider_remoteServerEnv(configDir),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_noop.noop", "remote", "tf-remote-pass-fqdn"),
					resource.TestCheckResourceAttr("lxd_noop.noop", "project", "default"),
					resource.TestCheckResourceAttrSet("lxd_noop.noop", "server_version"),
				),
			},
		},
	})
}

func TestAccProvider_generateClientCertificate(t *testing.T) {
	configDir := t.TempDir()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Ensure certificates are missing.
				Config: testAccProvider_localServer(configDir, false),
				Check: resource.ComposeTestCheckFunc(
					testCheckClientCert(configDir, false),
				),
			},
			{
				// Ensure certificates are generated.
				Config: testAccProvider_localServer(configDir, true),
				Check: resource.ComposeTestCheckFunc(
					testCheckClientCert(configDir, true),
				),
			},
		},
	})
}

func TestAccProvider_acceptRemoteCertificate(t *testing.T) {
	token := acctest.ConfigureTrustToken(t)
	configDir := t.TempDir()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.PreCheck(t)
			acctest.PreCheckLocalServerHTTPS(t)
		},
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Ensure authentication fails if remote server is not accepted.
				Config:      testAccProvider_remoteServer(configDir, "", token, false),
				ExpectError: regexp.MustCompile(`Failed to accept server certificate`),
			},
			{
				// Ensure authentication succeeds if remote server is accepted.
				Config: testAccProvider_remoteServer(configDir, "", token, true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_noop.noop", "remote", "tf-remote"),
					resource.TestCheckResourceAttr("lxd_noop.noop", "project", "default"),
					resource.TestCheckResourceAttrSet("lxd_noop.noop", "server_version"),
				),
			},
		},
	})
}

func testAccProvider_configDir(configDir string) string {
	return fmt.Sprintf(`
provider "lxd" {
  generate_client_certificates = true
  config_dir                   = %q
}

resource "lxd_noop" "noop" {
}
	`, configDir)
}

func testAccProvider_localServer(configDir string, generateClientCert bool) string {
	return fmt.Sprintf(`
provider "lxd" {
  generate_client_certificates = %v
  accept_remote_certificate    = true
  config_dir                   = %q
}

resource "lxd_noop" "noop" {
}
	`, generateClientCert, configDir)
}

func testAccProvider_remoteServer(configDir string, password string, token string, acceptRemoteCert bool) string {
	// Trust password and token are mutually exclusive in the configuration.
	authField := ""
	if password != "" {
		authField = fmt.Sprintf("password = %q", password)
	} else if token != "" {
		authField = fmt.Sprintf("token = %q", token)
	}

	return fmt.Sprintf(`
provider "lxd" {
  config_dir                   = %q
  generate_client_certificates = true
  accept_remote_certificate    = %v

  remote {
    name     = "tf-remote"
    protocol = "lxd"
    address  = "https://127.0.0.1:8443"
    %s
  }
}

resource "lxd_noop" "noop" {
  remote = "tf-remote"
}
	`, configDir, acceptRemoteCert, authField)
}

func testAccProvider_remoteServerEnv(configDir string) string {
	return fmt.Sprintf(`
provider "lxd" {
  generate_client_certificates = true
  accept_remote_certificate    = true
  config_dir  	       	       = %q
}

resource "lxd_noop" "noop" {
}
	`, configDir)
}

// testCheckClientCert checks that the client certificate was generated.
func testCheckClientCert(configDir string, shouldExist bool) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		for _, fileName := range []string{"client.crt", "client.key"} {
			_, err := os.Stat(filepath.Join(configDir, fileName))

			if shouldExist && err != nil {
				return fmt.Errorf("File %q not found: %w", fileName, err)
			}

			if !shouldExist && err == nil {
				return fmt.Errorf("File %q should not exist", fileName)
			}
		}

		return nil
	}
}

// resetLXDRemoteEnvVars unsets all environment variables that are supported by
// the provider.
func resetLXDRemoteEnvVars() {
	os.Unsetenv("LXD_REMOTE")
	os.Unsetenv("LXD_ADDR")
	os.Unsetenv("LXD_PASSWORD")
	os.Unsetenv("LXD_TOKEN")
}
