//go:build !unix

package main

// noFollow has no open(2) flag here; openLog's Lstat check covers it.
const noFollow = 0
