terraform {
  required_providers {
    gitlabcommits = {
      source  = "akselleirv/local/gitlabcommits"
      version = "0.0.1"
    }
  }
}

variable "gitlab_api_token" {
  type = string
}

variable "project_id" {
  type = string
}

provider "gitlabcommits" {
  gitlab_api_token = var.gitlab_api_token
  project_id       = var.project_id
  branch           = "main"
  author_email     = "akselleirv@example.com"
  author_name      = "Aksel"
  commit_message   = "I can add many files"
}

locals {
  files = [
    { path : "dir/file-1.txt", content : "some text 1" },
    { path : "dir/file-2.txt", content : "some text 2" },
    { path : "dir/file-3.txt", content : "some text 3" },
  ]
}

resource "gitlabcommits_file" "example" {
  for_each  = {for file in local.files : file.path => file.content}
  file_path = each.key
  content   = each.value
}