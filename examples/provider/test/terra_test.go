package test

import (
	"encoding/base64"
	"fmt"
	"github.com/gruntwork-io/terratest/modules/random"
	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/stretchr/testify/assert"
	"github.com/xanzy/go-gitlab"
	"os"
	"testing"
	"time"
)

// Why use terratest?
// Terraform acceptance test did not support resources with for_each in them.

func TestResourceWithForEach(t *testing.T) {
	filePaths := mockFilePaths()
	token, projectId := mustGetEnv(t, "GITLAB_TOKEN"), mustGetEnv(t, "PROJECT_ID")
	opts := &terraform.Options{
		TerraformDir: "../",
		Vars: map[string]interface{}{
			"files": filePaths,
		},
		EnvVars: map[string]string{
			"TF_VAR_gitlab_api_token": token,
			"TF_VAR_project_id":       projectId,
		},
	}

	defer terraform.Destroy(t, opts)
	terraform.InitAndApply(t, opts)

	c, err := gitlab.NewClient(token)
	if err != nil {
		t.Fatal(err)
	}

	// wait for gitlab to update
	time.Sleep(1 * time.Second)
	validateFilesExist(t, filePaths, c, projectId)
}

func validateFilesExist(t *testing.T, paths []string, c *gitlab.Client, projectId string) {
	for _, path := range paths {
		file, _, err := c.RepositoryFiles.GetFile(projectId, path, &gitlab.GetFileOptions{Ref: gitlab.String("main")})
		assert.NoError(t, err)
		decodedContent, err := base64.StdEncoding.DecodeString(file.Content)
		assert.NoError(t, err)
		assert.Contains(t, string(decodedContent), "content #")
	}
}

func mustGetEnv(t *testing.T, key string) string {
	v := os.Getenv(key)
	if v == "" {
		t.Fatalf("missing required env key '%s'", key)
	}
	return v
}

func mockFilePaths() []string {
	var paths []string
	for i := 0; i < 11; i++ {
		paths = append(paths, fmt.Sprintf("terratest/file-%s.txt", random.UniqueId()))
	}
	return paths
}
