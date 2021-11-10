package provider

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/avast/retry-go"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/xanzy/go-gitlab"
	"net/http"
	"os"
	"time"
)

func resourceGitlabCommit() *schema.Resource {
	return &schema.Resource{
		// This description is used by the documentation generator and the language server.
		Description: "The file resource will store a file in a repository based on the provided Gitlab project ID.",

		CreateContext: resourceGitlabcommitCreate,
		ReadContext:   resourceGitlabcommitRead,
		UpdateContext: resourceGitlabcommitUpdate,
		DeleteContext: resourceGitlabcommitDelete,

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

func resourceGitlabcommitRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*client)
	filePath := d.Id()

	repositoryFile, err := getFile(filePath, client.branch, client.projectId, client.gitlab)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logD(fmt.Sprintf("file %s not found, removing from state", filePath))
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
	d.Set("content", string(content))

	return nil
}

func resourceGitlabcommitCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	err := applyAction(gitlab.FileAction(gitlab.FileCreate), meta.(*client), d)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(d.Get("file_path").(string))
	d.Set("content", d.Get("content"))

	return resourceGitlabcommitRead(ctx, d, meta)
}

func resourceGitlabcommitUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	err := applyAction(gitlab.FileAction(gitlab.FileUpdate), meta.(*client), d)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(d.Get("file_path").(string))
	d.Set("content", d.Get("content"))
	return resourceGitlabcommitRead(ctx, d, meta)
}

func resourceGitlabcommitDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	err := applyAction(gitlab.FileAction(gitlab.FileDelete), meta.(*client), d)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")
	return nil
}

func applyAction(action *gitlab.FileActionValue, client *client, d *schema.ResourceData) error {
	filePath := d.Get("file_path").(string)
	content := d.Get("content").(string)

	gitlabAction := &gitlab.CommitActionOptions{
		Action:   action,
		FilePath: gitlab.String(filePath),
		Content:  gitlab.String(content),
	}

	logD("[RESOURCE] applying " + *gitlabAction.FilePath)
	client.actionCh <- gitlabAction

	return waitForResponse(
		*gitlabAction.FilePath,
		client.responseSyncCh,
	)
}

// waitForResponse listens for response from the actionSyncronizer
func waitForResponse(filePath string, responseSyncCh chan *responseSync) error {
	logD("[RESOURCE] will start waiting for response " + filePath)
	for {
		resp := <-responseSyncCh
		if resp.filePath == filePath {
			logD("[RESOURCE] received my own filepath: " + resp.filePath)
			if resp.err != nil {
				return resp.err
			}
			return nil
		}
		logD("[RESOURCE] resource '" + filePath + "' got '" + resp.filePath + "' sending back to synchronizer")
		responseSyncCh <- resp
	}
}

func getFile(filePath, branch, projectId string, client *gitlab.Client) (*gitlab.File, error) {
	options := &gitlab.GetFileOptions{
		Ref: gitlab.String(branch),
	}
	var repositoryFile *gitlab.File
	var resp *gitlab.Response

	// A resource might finish before the provider commits the files therefore we need to retry until file is committed
	err := retry.Do(func() error {
		var err error
		repositoryFile, resp, err = client.RepositoryFiles.GetFile(projectId, filePath, options)
		if err != nil {
			if resp.StatusCode == http.StatusNotFound {
				return os.ErrNotExist
			}
			return err
		}
		return nil
	},
		retry.MaxDelay(600*time.Millisecond),
		retry.Delay(300*time.Millisecond),
		retry.RetryIf(func(err error) bool {
			return errors.Is(err, os.ErrNotExist)
		}))

	return repositoryFile, err
}
