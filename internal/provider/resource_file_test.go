package provider

import (
	"errors"
	"fmt"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/stretchr/testify/assert"
	"github.com/xanzy/go-gitlab"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func TestAccResourceFile_create_one_file(t *testing.T) {
	client := testAccProvider.Meta().(*client)
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		CheckDestroy:      testAccCheckgitlabcommitFileDestroy(client, "gitlabcommit_file.test", client.projectId),
		Steps: []resource.TestStep{
			{
				Config: testAccResourceFileSimple(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckgitlabcommitFileExists(client, "gitlabcommit_file.test", client.projectId),
				),
			},
		},
	})
}

/*
This test is not supported. See issue: https://github.com/hashicorp/terraform-plugin-sdk/issues/536
func TestAccResourceFile_create_many_files(t *testing.T) {
	client := testAccProvider.Meta().(*client)
	fileNames := []string{"dir/file-" + acctest.RandString(4), "dir/file-" + acctest.RandString(4), "dir/file-" + acctest.RandString(4)}
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		CheckDestroy:      testAccCheckgitlabcommitFileDestroyMany(client, fileNames, client.projectId),
		Steps: []resource.TestStep{
			{
				Config: testAccResourceFileMany(fileNames[0], fileNames[1], fileNames[2]),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckgitlabcommitFileExistsMany(client, fileNames, client.projectId),
				),
			},
		},
	})
}

*/

func TestWaitForResponse(t *testing.T) {
	var (
		numberOfResources = 10
		debounce          = 50 * time.Millisecond

		actionCh     = make(chan *gitlab.CommitActionOptions)
		doneFilePath = make(chan string)
		errCh        = make(chan error)

		resourceWaitGroup           = &sync.WaitGroup{}
		actionSynchronizerWaitGroup = &sync.WaitGroup{}

		haltedResourceFilePath string
		errorsReceived         []error
		actualActions          []*gitlab.CommitActionOptions
		inputActions           []*gitlab.CommitActionOptions
	)
	// Create mock data
	for i := 0; i < numberOfResources; i++ {
		inputActions = append(inputActions, &gitlab.CommitActionOptions{
			Action:   gitlab.FileAction(gitlab.FileCreate),
			FilePath: gitlab.String(fmt.Sprintf("path/text-%d.txt", i)),
		})
	}

	// Start action synchronizer
	actionSynchronizerWaitGroup.Add(1)
	go func() {
		defer actionSynchronizerWaitGroup.Done()
		haltedResourceFilePath, actualActions = actionSyncronizer(debounce, actionCh, doneFilePath)
	}()

	// Start goroutines that is listening on channels
	resourceWaitGroup.Add(numberOfResources)
	for i := 0; i < numberOfResources; i++ {
		go func(index int, filePath string) {
			defer resourceWaitGroup.Done()
			actionCh <- inputActions[index]
			errorsReceived = append(errorsReceived, waitForResponse(filePath, doneFilePath, errCh))
		}(i, *inputActions[i].FilePath)
	}

	// validate synchronizer output
	actionSynchronizerWaitGroup.Wait()
	assert.Contains(t, haltedResourceFilePath, "path/text-")
	assert.ElementsMatch(t, inputActions, actualActions)

	// validate if error chan is working as expected
	err := errors.New("expected test error")
	errCh <- err
	resourceWaitGroup.Wait()
	// only last error should contain an error
	for _, err := range errorsReceived[:numberOfResources-1] {
		assert.Nil(t, err)
	}

	assert.Error(t, errorsReceived[numberOfResources-1])

}

func testAccResourceFileSimple() string {
	return fmt.Sprintf(`
resource "gitlabcommit_file" "test" {
  file_path = "dir/test-%d.txt"
  content   = "this is a test file #%d"
}
`, acctest.RandInt(), acctest.RandInt())
}

func testAccResourceFileMany(fileNameOne, fileNameTwo, fileNameThree string) string {
	return fmt.Sprintf(`
locals {
  files = [
    { path : "%s", content : "some text 1" },
    { path : "%s", content : "some text 2" },
    { path : "%s", content : "some text 3" },
  ]
}
resource "gitlabcommit_file" "test" {
  for_each  = {for file in local.files : file.path => file.content}
  file_path = each.key
  content   = each.value
}`, fileNameOne, fileNameTwo, fileNameThree)
}

func testAccCheckgitlabcommitFileExists(c *client, resourceName, projectId string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}
		fileId := rs.Primary.ID
		err := fileExist(c, fileId, projectId)
		if err != nil {
			return err
		}
		return nil
	}
}

func testAccCheckgitlabcommitFileDestroy(c *client, resourceName, projectId string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}
		err := fileExist(c, rs.Primary.ID, projectId)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		return errors.New("expected to not find any file")
	}
}

func testAccCheckgitlabcommitFileExistsMany(c *client, filePaths []string, projectId string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		for _, fileName := range filePaths {
			err := fileExist(c, fileName, projectId)
			if err != nil {
				return err
			}
		}

		return nil
	}
}

func testAccCheckgitlabcommitFileDestroyMany(c *client, filePaths []string, projectId string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		for _, fileName := range filePaths {
			err := fileExist(c, fileName, projectId)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				return err
			}
		}
		return errors.New("expected to not find any file")
	}
}

func fileExist(c *client, fileId, projectId string) error {
	_, _, err := c.gitlab.RepositoryFiles.GetFile(projectId, fileId, &gitlab.GetFileOptions{Ref: gitlab.String("main")})
	if err != nil {
		if strings.Contains(err.Error(), "404 File Not Found") {
			return os.ErrNotExist
		}
		return fmt.Errorf("cannot get file: %v", err)
	}
	return nil
}
