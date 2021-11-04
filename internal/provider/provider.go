package provider

import (
	"context"
	"fmt"
	"github.com/avast/retry-go"
	"github.com/xanzy/go-gitlab"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func init() {
	// Set descriptions to support markdown syntax, this will be used in document generation
	// and the language server.
	schema.DescriptionKind = schema.StringMarkdown

	// Customize the content of descriptions when output. For example you can add defaults on
	// to the exported descriptions if present.
	// schema.SchemaDescriptionBuilder = func(s *schema.Schema) string {
	// 	desc := s.Description
	// 	if s.Default != nil {
	// 		desc += fmt.Sprintf(" Defaults to `%v`.", s.Default)
	// 	}
	// 	return strings.TrimSpace(desc)
	// }
}

func New() *schema.Provider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"gitlab_api_token": {
				Type:        schema.TypeString,
				Required:    true,
				Sensitive:   true,
				DefaultFunc: schema.EnvDefaultFunc("GITLAB_TOKEN", nil),
			},
			"project_id": {
				Type:        schema.TypeString,
				Required:    true,
				DefaultFunc: schema.EnvDefaultFunc("PROJECT_ID", nil),
			},
			"branch": {
				Type:     schema.TypeString,
				Optional: true,
				Default:  "main",
				ForceNew: true,
			},
			"start_branch": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"author_email": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"author_name": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"commit_message": {
				Type:     schema.TypeString,
				Optional: true,
				Default:  "terraform-provider-gitlabcommit",
			},
			"debounce_time": {
				Type:        schema.TypeInt,
				Optional:    true,
				Default:     2000,
				Description: "How long the provider should wait for the resources before sending the commit. Value is given in milliseconds.",
			},
		},
		ConfigureContextFunc: configure,
		ResourcesMap: map[string]*schema.Resource{
			"gitlabcommit_file": resourceGitlabCommit(),
		},
	}

}

type client struct {
	gitlab *gitlab.Client

	projectId string

	branch string

	actionCh chan<- *gitlab.CommitActionOptions

	// doneFilePath is used to tell the resource that they can exit if the filePath is theirs
	doneFilePath chan string

	// errCh is used for communicate errors if commit fails
	errCh <-chan error
}

func configure(ctx context.Context, d *schema.ResourceData) (interface{}, diag.Diagnostics) {
	var (
		actionCh     = make(chan *gitlab.CommitActionOptions)
		doneFilePath = make(chan string)
		errCh        = make(chan error)
	)

	gitlabClient, err := gitlab.NewClient(d.Get("gitlab_api_token").(string))
	if err != nil {
		return nil, diag.FromErr(err)
	}

	go handleResources(d, gitlabClient, actionCh, doneFilePath, errCh)

	logD("done configuring provider")
	return &client{
		gitlab:       gitlabClient,
		projectId:    d.Get("project_id").(string),
		branch:       d.Get("branch").(string),
		doneFilePath: doneFilePath,
		actionCh:     actionCh,
		errCh:        errCh,
	}, nil

}

func handleResources(d *schema.ResourceData, c *gitlab.Client, actionCh <-chan *gitlab.CommitActionOptions, doneFilePath chan<- string, errCh chan<- error) {
	duration := time.Duration(d.Get("debounce_time").(int))
	debounceDuration := duration * time.Millisecond
	_, commitActions := actionSyncronizer(debounceDuration, actionCh, doneFilePath)
	err := doCommits(d.Get("project_id").(string), c, &gitlab.CreateCommitOptions{
		Actions:       commitActions,
		Branch:        gitlab.String(d.Get("branch").(string)),
		AuthorEmail:   gitlab.String(d.Get("author_email").(string)),
		AuthorName:    gitlab.String(d.Get("author_name").(string)),
		CommitMessage: gitlab.String(d.Get("commit_message").(string)),
	})

	if err != nil {
		logD("received an error when sending commit: " + err.Error())
		errCh <- err
		logD("successfully sent the error on the the errCh")
		return
	}
	errCh <- nil
	logD("successfully sent commit to gitlab api")
}

// actionSyncronizer will collect all gitlab.CommitActionOptions and return them in a slice when time since last resource received is bigger than debounce time.
// The done channel is used to halt the first resource to avoid Terraform from exiting.
func actionSyncronizer(debounce time.Duration, actionCh <-chan *gitlab.CommitActionOptions, filePathDone chan<- string) (string, []*gitlab.CommitActionOptions) {
	var (
		actionsToSend  []*gitlab.CommitActionOptions
		haltedResource string
		timeNow        = time.Now()
		ticker         = time.NewTicker(debounce / 2)
	)

	defer ticker.Stop()

LOOP:
	for {
		select {
		case action := <-actionCh:
			logD("[PROVIDER] received filePath for: " + *action.FilePath)
			timeNow = time.Now()
			actionsToSend = append(actionsToSend, action)

			if haltedResource == "" {
				logD("[PROVIDER] halting resource with filepath: " + *action.FilePath)
				// we halt this resource to avoid terraform exiting
				haltedResource = *action.FilePath
			} else {
				logD("[PROVIDER] sending done to filepath: " + *action.FilePath)
				// but we let the other resource exit
				filePathDone <- *action.FilePath
			}
			logD("[PROVIDER] total received actions: " + strconv.Itoa(len(actionsToSend)))
		case <-ticker.C:
			logD("[PROVIDER] new tick")
			if time.Since(timeNow) > debounce {
				logD("[PROVIDER] breaking loop due to time since last received action is greater than debounce time")
				break LOOP
			}
		}
	}

	return haltedResource, actionsToSend
}

func doCommits(projectId string, c *gitlab.Client, opts *gitlab.CreateCommitOptions) error {
	if len(opts.Actions) == 0 {
		logD("skipping commit due no actions")
		return nil
	}
	logD(fmt.Sprintf("creating commits for %d actions", len(opts.Actions)))

	return retry.Do(
		func() error {
			_, resp, err := c.Commits.CreateCommit(projectId, opts)
			if err != nil {
				if strings.Contains(err.Error(), "A file with this name already exists") {
					return nil
				}
				return fmt.Errorf("unable to create commit: status message %s: status code %d: %w", resp.Status, resp.StatusCode, err)
			}
			return nil
		},
		retry.RetryIf(func(err error) bool {
			// This error can happen if ref was updated at the same time as commit was pushed.
			return strings.Contains(err.Error(), fmt.Sprintf("Could not update refs/heads/%s. Please refresh and try again..", *opts.Branch))
		}),
		retry.Delay(1*time.Second),
		retry.MaxDelay(3*time.Second),
	)
}

func logD(v string) {
	log.Println("[DEBUG] " + v)
}
