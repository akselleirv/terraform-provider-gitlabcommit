# Terraform Provider Gitlabcommits

Can add one or more files using the `for_each` meta-argument on `gitlabcommits_file`resource.

# Motivation

As of writing there is one pull request for adding file resource for the offical Gitlab terraform provider, but is has a
limitation of only being able to add one file at a time or set `-parallelism=1`. This provider collects create, update
and delete actions and commits all changes in one commit.

# Known issues

### Confusing error message

The provider will halt one resource in order to not be exited by Terraform. This results in possible error messages
being displayed for that specific resource while the error might be for another resource in the `for_each` collection.

### Incorrect state

This issue is also due to the resource halting. The resources that is not halted by the provider will display that they
were applied, but this might be incorrect if the commit failed. This will hopefully be corrected when a read/plan is
executed.