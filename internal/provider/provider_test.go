package provider

import (
	"context"
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
		debounce       = 50 * time.Millisecond
		actionCh       = make(chan *gitlab.CommitActionOptions)
		done           = make(chan string)
		wg             = sync.WaitGroup{}
	)

	actions := []*gitlab.CommitActionOptions{
		{
			Action:   gitlab.FileAction(gitlab.FileCreate),
			FilePath: gitlab.String("path/text-0.txt"),
		}, {
			Action:   gitlab.FileAction(gitlab.FileCreate),
			FilePath: gitlab.String("path/text-1.txt"),
		},
	}

	start := time.Now()
	wg.Add(1)
	go func() {
		defer wg.Done()
		resourceHalted, actualActions = actionSyncronizer(debounce, actionCh, done)
	}()

	actionCh <- actions[0]
	actionCh <- actions[1]

	actualFilePathDone := <-done
	assert.Equal(t, *actions[1].FilePath, actualFilePathDone)
	assert.Equal(t, "", resourceHalted, "this resource should be halted")
	assert.Nil(t, actualActions, "the collected actions should not be sent yet")

	wg.Wait()
	within50Milli := time.Now().Add(time.Millisecond * -50)
	assert.WithinDuration(t, within50Milli, start, 5*time.Millisecond)
	assert.Equal(t, actions, actualActions)
	assert.Equal(t, *actions[0].FilePath, resourceHalted)
}
