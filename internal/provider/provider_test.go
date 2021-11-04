package provider

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/stretchr/testify/assert"
	"github.com/xanzy/go-gitlab"
	"os"
	"sync"
	"testing"
	"time"
)

var testAccProvider *schema.Provider
var testAccProviderFactories = map[string]func() (*schema.Provider, error){
	"gitlabcommit": func() (*schema.Provider, error) {
		return New(), nil
	},
}

func init() {
	testAccProvider = New()
	testAccProvider.Configure(context.Background(), &terraform.ResourceConfig{})
	testAccProviderFactories = map[string]func() (*schema.Provider, error){
		"gitlabcommit": func() (*schema.Provider, error) {
			return testAccProvider, nil
		},
	}
}

func TestProvider(t *testing.T) {
	if err := New().InternalValidate(); err != nil {
		t.Fatalf("err: %s", err)
	}
}

func testAccPreCheck(t *testing.T) {
	if v := os.Getenv("GITLAB_TOKEN"); v == "" {
		t.Fatalf("GITLAB_TOKEN env var must be set for acceptance test")
	}
	if v := os.Getenv("PROJECT_ID"); v == "" {
		t.Fatalf("PROJECT_ID env var must be for acceptance test")
	}
}

func TestActionSyncronizer(t *testing.T) {
	var (
		resourceHalted string
		actualActions  []*gitlab.CommitActionOptions
		inputActions   []*gitlab.CommitActionOptions
		debounce       = 50 * time.Millisecond
		actionCh       = make(chan *gitlab.CommitActionOptions)
		filePathDone   = make(chan string)
		wg             = sync.WaitGroup{}
	)

	for i := 0; i < 10; i++ {
		inputActions = append(inputActions, &gitlab.CommitActionOptions{
			Action:   gitlab.FileAction(gitlab.FileCreate),
			FilePath: gitlab.String(fmt.Sprintf("path/text-%d.txt", i)),
		})
	}

	start := time.Now()
	wg.Add(1)
	go func() {
		defer wg.Done()
		resourceHalted, actualActions = actionSyncronizer(debounce, actionCh, filePathDone)
	}()

	for i, action := range inputActions {
		actionCh <- action

		if i != 0 {
			actualFilePathDone := <-filePathDone
			assert.Equal(t, *inputActions[i].FilePath, actualFilePathDone)
			assert.Equal(t, "", resourceHalted, "this resource should be halted")
			assert.Nil(t, actualActions, "the collected actions should not be sent yet")
		}
	}

	wg.Wait()
	within50Milli := time.Now().Add(time.Millisecond * -50)
	assert.WithinDuration(t, within50Milli, start, 30*time.Millisecond)
	assert.Equal(t, inputActions, actualActions)
	assert.Equal(t, *inputActions[0].FilePath, resourceHalted)
}
