//go:build unix

package main

import "syscall"

// noFollow makes opening agent.log fail when it is a symlink: whoever can
// write the state directory must not be able to point the log of a root
// service at /etc/crontab or a shell profile.
const noFollow = syscall.O_NOFOLLOW
