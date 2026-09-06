// Package playbookrun runs the card entries of a runnable playbook in order.
package playbookrun

// LaunchOptions is what a playbook launch adds to a normal card trigger: the
// backend creates the playbook base branch from BaseBranchFrom (empty = the
// remote default) when it does not exist yet.
type LaunchOptions struct {
	CreateBaseBranch bool
	BaseBranchFrom   string
}
