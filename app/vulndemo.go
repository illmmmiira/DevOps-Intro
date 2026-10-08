package main

import "golang.org/x/text/language"

// TEMPORARY: reachable call into a vulnerable version (GO-2021-0113), to prove the gate works.
func init() { _, _ = language.Parse("en") }
