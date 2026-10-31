//go:build unix

package vault

import "syscall"

// openNonblock keeps open(2) from blocking on a FIFO; ReadNote rejects
// non-regular files right after the open.
const openNonblock = syscall.O_NONBLOCK
