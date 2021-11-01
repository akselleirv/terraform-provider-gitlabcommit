locals {
  files = [
    { path : "dir/file-1.txt", content : "some text 1" },
    { path : "dir/file-2.txt", content : "some text 2" },
    { path : "dir/file-3.txt", content : "some text 3" },
  ]
}

resource "gitlabcommit_file" "example" {
  for_each  = { for file in local.files : file.path => file.content }
  file_path = each.key
  content   = each.value
}