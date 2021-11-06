terraform {
  required_providers {
    gitlabcommit = {
      source  = "akselleirv/local/gitlabcommit"
      version = ">=0.0.1"
    }
  }
}

variable "gitlab_api_token" {
  type = string
}

variable "project_id" {
  type = string
}

variable "files" {
  type = list(string) // a list of file paths
}

provider "gitlabcommit" {
  gitlab_api_token = var.gitlab_api_token
  project_id       = var.project_id
  branch           = "main"
  author_email     = "akselleirv@example.com"
  author_name      = "Aksel"
  commit_message   = "I can add many files!"
}


resource "gitlabcommit_file" "example" {
  for_each  = {for idx, filepath in var.files : filepath => "content #${idx} - ${uuid()}"}
  file_path = each.key
  content   = each.value
}