package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/xanzy/go-gitlab"
	"log"
	"strings"
)

func resourceGitlabCommits() *schema.Resource {
	return &schema.Resource{
		// This description is used by the documentation generator and the language server.
		Description: "The file resource will store a file in a repository based on the provided Gitlab project ID.",

		CreateContext: resourceGitlabCommitsCreate,
		ReadContext:   resourceGitlabCommitsRead,
		UpdateContext: resourceGitlabCommitsUpdate,
		DeleteContext: resourceGitlabCommitsDelete,

		Schema: map[string]*schema.Schema{
			"file_path": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"content": {
				Type:     schema.TypeString,
				Required: true,
			},
		},
	}
}

func resourceGitlabCommitsCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	err := applyAction(gitlab.FileAction(gitlab.FileCreate), meta.(*client), d)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(d.Get("file_path").(string))

	return nil
}

func resourceGitlabCommitsRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*client)
	filePath := d.Get("file_path").(string)
	options := &gitlab.GetFileOptions{
		// TODO: send branch here from provider
		Ref: gitlab.String("main"),
	}

	repositoryFile, _, err := client.gitlab.RepositoryFiles.GetFile(client.projectId, filePath, options)
	if err != nil {
		if strings.Contains(err.Error(), "404 File Not Found") {
			log.Printf("[WARN] file %s not found, removing from state", filePath)
			d.SetId("")
			return nil
		}
		return diag.FromErr(err)
	}

	d.SetId(repositoryFile.FilePath)
	content, err := base64.StdEncoding.DecodeString(repositoryFile.Content)
	if err != nil {
		return diag.FromErr(fmt.Errorf("unable to decode content: %w", err))
	}
	if err := d.Set("content", string(content)); err != nil {
		return diag.FromErr(err)
	}

	return nil
}

func resourceGitlabCommitsUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	err := applyAction(gitlab.FileAction(gitlab.FileUpdate), meta.(*client), d)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")
	return nil
}

func resourceGitlabCommitsDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	err := applyAction(gitlab.FileAction(gitlab.FileDelete), meta.(*client), d)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")
	return nil
}

// waitForResponse listens for response from the actionSyncronizer
func waitForResponse(filePath string, doneFilePath chan string, errCh <-chan error) error {
	for {
		select {
		case filePathReceived := <-doneFilePath:
			if filePathReceived == filePath {
				log.Println("received my own filepath: ", filePathReceived)
				break
			}
			go func() {
				// TODO: check if this goroutine is not needed
				log.Println("resending file back to synchronizer: ", filePathReceived)
				doneFilePath <- filePathReceived
			}()
			return nil
		case err := <-errCh:
			return err
		}
	}
}

func applyAction(action *gitlab.FileActionValue, client *client, d *schema.ResourceData) error {
	filePath := d.Get("file_path").(string)
	content := d.Get("content").(string)

	gitlabAction := &gitlab.CommitActionOptions{
		Action:   action,
		FilePath: gitlab.String(filePath),
		Content:  gitlab.String(content),
	}

	client.actionCh <- gitlabAction

	return waitForResponse(
		*gitlabAction.FilePath,
		client.doneFilePath,
		client.errCh,
	)
}
