package shell

//go:generate go run ./generate

/*
The events a menu item announces, as the frontend receives them.

Written to TypeScript by `go generate ./internal/shell` rather than copied by
hand: these strings are a contract between two languages, and a contract kept
in two places is one that eventually disagrees with itself. The generated file
is committed, so a checkout builds without running the generator.
*/
