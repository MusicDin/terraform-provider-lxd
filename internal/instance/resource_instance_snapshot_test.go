package instance_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/terraform-lxd/terraform-provider-lxd/internal/acctest"
)

func TestAccInstanceSnapshot_stateless(t *testing.T) {
	instanceName := acctest.GenerateName(2, "-")
	snapshotName := acctest.GenerateName(2, "-")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceSnapshot_basic(instanceName, snapshotName, false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_instance.instance1", "name", instanceName),
					resource.TestCheckResourceAttr("lxd_instance.instance1", "status", "Running"),
					resource.TestCheckResourceAttr("lxd_snapshot.snapshot1", "name", snapshotName),
					resource.TestCheckResourceAttr("lxd_snapshot.snapshot1", "instance", instanceName),
					resource.TestCheckResourceAttr("lxd_snapshot.snapshot1", "stateful", "false"),
					resource.TestCheckResourceAttrSet("lxd_snapshot.snapshot1", "created_at"),
				),
			},
		},
	})
}

// TODO: Test requires CRIU
// func TestAccInstanceSnapshot_stateful(t *testing.T) {
// 	instanceName := acctest.GenerateName(2, "-")
// 	snapshotName := acctest.GenerateName(2, "-")

// 	resource.Test(t, resource.TestCase{
// 		PreCheck:                 func() { acctest.PreCheck(t) },
// 		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
// 		Steps: []resource.TestStep{
// 			{
// 				Config: testAccInstanceSnapshot_basic(instanceName, snapshotName, true),
// 				Check: resource.ComposeTestCheckFunc(
// 					resource.TestCheckResourceAttr("lxd_instance.instance1", "name", instanceName),
// 					resource.TestCheckResourceAttr("lxd_instance.instance1", "status", "Running"),
// 					resource.TestCheckResourceAttr("lxd_snapshot.snapshot1", "name", snapshotName),
// 					resource.TestCheckResourceAttr("lxd_snapshot.snapshot1", "stateful", "true"),
// 					resource.TestCheckResourceAttrSet("lxd_snapshot.snapshot1", "created_at"),
// 				),
// 			},
// 		},
// 	})
// }

func TestAccInstanceSnapshot_multiple(t *testing.T) {
	instanceName := acctest.GenerateName(2, "-")
	snapshotName1 := acctest.GenerateName(2, "-")
	snapshotName2 := acctest.GenerateName(2, "-")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceSnapshot_multiple1(instanceName, snapshotName1),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_instance.instance1", "name", instanceName),
					resource.TestCheckResourceAttr("lxd_instance.instance1", "status", "Running"),
					resource.TestCheckResourceAttr("lxd_snapshot.snapshot1", "name", snapshotName1),
					resource.TestCheckResourceAttr("lxd_snapshot.snapshot1", "instance", instanceName),
					resource.TestCheckResourceAttrSet("lxd_snapshot.snapshot1", "created_at"),
				),
			},
			{
				Config: testAccInstanceSnapshot_multiple2(instanceName, snapshotName1, snapshotName2),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_instance.instance1", "name", instanceName),
					resource.TestCheckResourceAttr("lxd_instance.instance1", "status", "Running"),
					resource.TestCheckResourceAttr("lxd_snapshot.snapshot1", "name", snapshotName1),
					resource.TestCheckResourceAttr("lxd_snapshot.snapshot1", "instance", instanceName),
					resource.TestCheckResourceAttrSet("lxd_snapshot.snapshot1", "created_at"),
					resource.TestCheckResourceAttr("lxd_snapshot.snapshot2", "name", snapshotName2),
					resource.TestCheckResourceAttr("lxd_snapshot.snapshot2", "instance", instanceName),
					resource.TestCheckResourceAttrSet("lxd_snapshot.snapshot2", "created_at"),
				),
			},
		},
	})
}

func TestAccInstanceSnapshot_project(t *testing.T) {
	projectName := acctest.GenerateName(2, "-")
	instanceName := acctest.GenerateName(2, "-")
	snapshotName := acctest.GenerateName(2, "-")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceSnapshot_project(projectName, instanceName, snapshotName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("lxd_project.project1", "name", projectName),
					resource.TestCheckResourceAttr("lxd_instance.instance1", "name", instanceName),
					resource.TestCheckResourceAttr("lxd_instance.instance1", "project", projectName),
					resource.TestCheckResourceAttr("lxd_instance.instance1", "status", "Running"),
					resource.TestCheckResourceAttr("lxd_snapshot.snapshot1", "name", snapshotName),
					resource.TestCheckResourceAttr("lxd_snapshot.snapshot1", "instance", instanceName),
					resource.TestCheckResourceAttr("lxd_snapshot.snapshot1", "project", projectName),
					resource.TestCheckResourceAttrSet("lxd_snapshot.snapshot1", "created_at"),
				),
			},
		},
	})
}

func TestAccInstanceSnapshot_missingInstance(t *testing.T) {
	instanceName := acctest.GenerateName(2, "-")
	snapshotName := acctest.GenerateName(2, "-")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      testAccInstanceSnapshot_missingInstance(instanceName, snapshotName),
				ExpectError: regexp.MustCompile("Instance not\nfound"),
			},
		},
	})
}

func testAccInstanceSnapshot_basic(cName, sName string, stateful bool) string {
	return fmt.Sprintf(`
resource "lxd_instance" "instance1" {
  name  = "%s"
  image = "%s"
}

resource "lxd_snapshot" "snapshot1" {
  instance = lxd_instance.instance1.name
  name     = "%s"
  stateful = "%v"
}
	`, cName, acctest.TestImage, sName, stateful)
}

func testAccInstanceSnapshot_multiple1(cName, sName string) string {
	return fmt.Sprintf(`
resource "lxd_instance" "instance1" {
  name  = "%s"
  image = "%s"
}

resource "lxd_snapshot" "snapshot1" {
  name     = "%s"
  instance = lxd_instance.instance1.name
  stateful = false
}
	`, cName, acctest.TestImage, sName)
}

func testAccInstanceSnapshot_multiple2(cName, sName1, sName2 string) string {
	return fmt.Sprintf(`
resource "lxd_instance" "instance1" {
  name = "%s"
  image = "%s"
}

resource "lxd_snapshot" "snapshot1" {
  name     = "%s"
  instance = lxd_instance.instance1.name
  stateful = "false"
}

resource "lxd_snapshot" "snapshot2" {
  name     = "%s"
  instance = lxd_instance.instance1.name
  stateful = "false"
}
	`, cName, acctest.TestImage, sName1, sName2)
}
func testAccInstanceSnapshot_project(project, instance, snapshot string) string {
	return fmt.Sprintf(`
resource "lxd_project" "project1" {
  name = "%s"
  config = {
    "features.storage.volumes" = false
    "features.images"          = false
    "features.profiles"        = false
  }
}

resource "lxd_instance" "instance1" {
  name    = "%s"
  image   = "%s"
  project = lxd_project.project1.name
}

resource "lxd_snapshot" "snapshot1" {
  name     = "%s"
  instance = lxd_instance.instance1.name
  stateful = false
  project  = lxd_project.project1.name
}
	`, project, instance, acctest.TestImage, snapshot)
}

func testAccInstanceSnapshot_missingInstance(cName, sName string) string {
	return fmt.Sprintf(`
resource "lxd_snapshot" "snapshot1" {
  instance = "%s"
  name     = "%s"
}
	`, cName, sName)
}
