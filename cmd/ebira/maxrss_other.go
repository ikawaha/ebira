//go:build !unix

package main

// maxRSS cannot read the resident set size here.
func maxRSS() int64 { return 0 }
