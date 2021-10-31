package provider

import (
	"context"
	"fmt"
	"github.com/xanzy/go-gitlab"
	"log"
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

func New(version string) func() *schema.Provider {
	return func() *schema.Provider {
		p := &schema.Provider{
			Schema: map[string]*schema.Schema{
				"gitlab_api_token": {
					Type:     schema.TypeString,
					Required: true,
				},
				"project_id": {
					Type:     schema.TypeString,
					Required: true,
				},
				"branch": {
					Type:     schema.TypeString,
					Required: true,
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
					Required: true,
				},
			},
			ResourcesMap: map[string]*schema.Resource{
				"gitlabcommits_file": resourceGitlabCommits(),
			},
		}

		p.ConfigureContextFunc = configure(version, p)

		return p
	}
}

type client struct {
	gitlab *gitlab.Client

	projectId string

	actionCh chan<- *gitlab.CommitActionOptions

	// doneFilePath is used to tell the resource that they can exit if the filePath is theirs
	doneFilePath chan string

	// errCh is used for communicate errors if commit fails
	errCh <-chan error
}

func configure(version string, p *schema.Provider) func(context.Context, *schema.ResourceData) (interface{}, diag.Diagnostics) {
	return func(ctx context.Context, d *schema.ResourceData) (interface{}, diag.Diagnostics) {
		gitlabApiToken := "gitlab_api_token"
		gitlabToken, ok := d.GetOk(gitlabApiToken)
		if !ok {
			return nil, diag.FromErr(fmt.Errorf("provider schema %s cannot be emtpy", gitlabApiToken))
		}

		gitlabClient, err := gitlab.NewClient(gitlabToken.(string))
		if err != nil {
			return nil, diag.FromErr(err)
		}

		actionCh := make(chan *gitlab.CommitActionOptions)
		doneFilePath := make(chan string)
		errCh := make(chan error)

		go func() {
			haltedResource, commitActions := actionSyncronizer(2*time.Second, actionCh, doneFilePath)
			defer func() {
				doneFilePath <- haltedResource
			}()
			if err := doCommits(d.Get("project_id").(string), gitlabClient, commitActions); err != nil {
				log.Printf("received an error when sending commit: %s", err.Error())
				errCh <- err
				log.Println("successfully sent the error on the the errCh")
				return
			}
			errCh <- nil
			log.Println("successfully sent commit to gitlab api")
		}()

		log.Println("[DEBUG] done configuring provider")
		return &client{
			gitlab:       gitlabClient,
			projectId:    d.Get("project_id").(string),
			doneFilePath: doneFilePath,
			actionCh:     actionCh,
			errCh:        errCh,
		}, nil
	}
}

// actionSyncronizer will collect all gitlab.CommitActionOptions and return them in a slice when time since last resource received is bigger than debounce time.
// The done channel is used to halt the first resource to avoid Terraform from exiting.
func actionSyncronizer(
	debounce time.Duration,
	actionCh <-chan *gitlab.CommitActionOptions,
	filePathDone chan<- string) (string, []*gitlab.CommitActionOptions) {

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
			log.Println("received filePath for filepath: ", *action.FilePath)
			timeNow = time.Now()
			actionsToSend = append(actionsToSend, action)

			if haltedResource == "" {
				log.Println("halting resource with filepath: ", *action.FilePath)
				// we halt this resource to avoid terraform exiting
				haltedResource = *action.FilePath
			} else {
				if *action.FilePath != haltedResource {
					log.Println("sending done to filepath: ", *action.FilePath)
					// but we let the other resource exit
					filePathDone <- *action.FilePath
				}

			}
		case <-ticker.C:
			log.Println("new tick")
			if time.Since(timeNow) > debounce {
				log.Println("breaking loop")
				break LOOP
			}
		}
	}

	return haltedResource, actionsToSend
}

func doCommits(projectId string, c *gitlab.Client, commitActions []*gitlab.CommitActionOptions) error {
	log.Printf("creating commits for %d actions", len(commitActions))

	_, resp, err := c.Commits.CreateCommit(projectId, &gitlab.CreateCommitOptions{
		// TODO: send config data from provider
		Branch:        gitlab.String("main"),
		CommitMessage: gitlab.String("commit_msg"),
		Actions:       commitActions,
	})
	if err != nil {
		if strings.Contains(err.Error(), "A file with this name already exists") {
			// TODO: Will this be handled by read function?
			// What happens on several files is commited, but only one files does not exist?
			return nil
		}

		return fmt.Errorf("unable to create commit: status message %s: status code %d: %w", resp.Status, resp.StatusCode, err)
	}

	return nil
}
