package commands

import "github.com/sethcarney/mdm/internal/lock"

// lockName threads the project lock file's name into help text and
// user-facing messages, so renaming the file stays a one-line change to
// lock.ProjectLockName.
const lockName = lock.ProjectLockName
