package provider

import (
	"github.com/stretchr/testify/assert"
	"github.com/xanzy/go-gitlab"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// providerFactories are used to instantiate a provider during acceptance testing.
// The factory function will be invoked for every Terraform CLI command executed
// to create a provider server to which the CLI can reattach.
var providerFactories = map[string]func() (*schema.Provider, error){
	"scaffolding": func() (*schema.Provider, error) {
		return New("dev")(), nil
	},
}

func TestProvider(t *testing.T) {
	if err := New("dev")().InternalValidate(); err != nil {
		t.Fatalf("err: %s", err)
	}
}

func testAccPreCheck(t *testing.T) {
	// You can add code here to run prior to any test case execution, for example assertions
	// about the appropriate environment variables being set are common to see in a pre-check
	// function.
}

func TestActionSyncronizer(t *testing.T) {
	var (
		resourceHalted   string
		actualActions    []*gitlab.CommitActionOptions
		debounce         = 50 * time.Millisecond
		actionCh         = make(chan *gitlab.CommitActionOptions)
		resendFilePathCh = make(chan string)
		done             = make(chan string)
		wg               = sync.WaitGroup{}
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
		resourceHalted, actualActions = actionSyncronizer(debounce, actionCh, resendFilePathCh, done)
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
