// Command commitcheck validates commit messages against Conventional Commits.
//
// The convention is not new here: PRD-25 RF-14 generates the changelog from the commits, so
// the format was already assumed. This is what makes it true, because a convention that
// nothing enforces is a convention that half the history does not follow, and a changelog
// built from that history is worse than no changelog -- it is a list with holes in it.
//
// It is written in Go for the same reason apicheck is: Go is the one toolchain this
// repository already requires, so the check runs on every machine and in CI without anybody
// installing Node.
//
// The rules, and why each one:
//
//		type(scope)!: subject
//
//	 1. The type comes from a fixed list. An open list means `chore`, `chores` and `misc` all
//	    appear and the changelog cannot group anything.
//	 2. The scope is optional and lowercase. It is free text on purpose: `tours`, `pkg/amqp`
//	    and `deploy` are all useful, and a closed list would need editing on every new
//	    package.
//	 3. `!` marks a breaking change, and it is the only marker that survives a squash.
//	 4. The subject is required, starts lowercase and does not end with a period. This is
//	    style, and the reason to enforce style is that a changelog is a list: entries that
//	    start differently and end differently read as noise.
//	 5. The header is at most 72 characters, so `git log --oneline` and the GitHub list stay
//	    readable without truncation.
//	 6. If there is a body, the second line is blank. Without it git treats the whole thing as
//	    one paragraph and every tool that reads a subject reads the body too.
//
// Usage:
//
//	commitcheck                 the commits of the current branch, against main
//	commitcheck <range>         a git revision range, e.g. main..HEAD
//	commitcheck --stdin         one message read from stdin (a PR title, a commit-msg hook)
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"
)

// errProblems is the single failure mode, wrapped with the count at the call site.
var errProblems = errors.New("commit message(s) do not follow Conventional Commits")

// types is the closed list. It is the Angular set, which is what every changelog generator
// already knows how to group.
var types = []string{
	"build",    // build system, dependencies, Dockerfiles
	"chore",    // anything that does not change src or tests
	"ci",       // workflows and pipeline
	"docs",     // documentation only
	"feat",     // a new feature -- minor version
	"fix",      // a bug fix -- patch version
	"perf",     // performance
	"refactor", // neither fixes a bug nor adds a feature
	"revert",   // reverts a previous commit
	"style",    // formatting, no behavior change
	"test",     // tests only
}

// headerPattern is the whole grammar. The scope allows dots, slashes and dashes so a Go
// package path (`pkg/amqp`) and a file (`go.mod`) are both expressible.
var headerPattern = regexp.MustCompile(
	`^(?P<type>[a-z]+)(?:\((?P<scope>[a-z0-9._/-]+)\))?(?P<breaking>!)?: (?P<subject>.+)$`,
)

// maxHeaderLength keeps `git log --oneline` readable. 72 is the usual limit; GitHub starts
// truncating around 70.
const maxHeaderLength = 72

// gitTimeout bounds the one outbound call this tool makes.
const gitTimeout = 30 * time.Second

func main() {
	args := os.Args[1:]

	var (
		messages []string
		err      error
	)

	switch {
	case len(args) > 0 && args[0] == "--stdin":
		var message string

		message, err = readStdin()
		messages = []string{message}

	default:
		revisionRange := "main..HEAD"
		if len(args) > 0 {
			revisionRange = args[0]
		}

		messages, err = readRange(revisionRange)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "\ncommitcheck: %v\n\n", err)
		os.Exit(1)
	}

	if err := check(messages); err != nil {
		os.Exit(1)
	}
}

// readStdin reads a single message, which is how the PR title and a commit-msg hook arrive.
func readStdin() (string, error) {
	var builder strings.Builder

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		builder.WriteString(scanner.Text())
		builder.WriteString("\n")
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading stdin: %w", err)
	}

	return builder.String(), nil
}

// readRange returns the full message of every commit in the range.
//
// The separator is a NUL byte because a commit message can contain anything else, blank
// lines included: splitting on one is how the body of one commit stops being read as the
// header of the next.
func readRange(revisionRange string) ([]string, error) {
	// A deadline on every outbound call, git included (hard rule 7). Reading a log is fast;
	// what this bounds is the case where the repository is on a stalled network mount and
	// the check hangs instead of failing.
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	// The range comes from the caller: a make target or a CI job, never from a commit or a
	// pull request title. It is passed as an argument to git, not to a shell.
	cmd := exec.CommandContext(ctx, "git", "log", "--format=%B%x00", revisionRange) //nolint:gosec // the range is not user input

	out, err := cmd.Output()
	if err != nil {
		// git puts the useful half on stderr: "unknown revision", "bad revision". Without
		// this the caller gets "exit status 128", which says nothing about the range they
		// typed wrong.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return nil, fmt.Errorf("reading the commits of %q: %w: %s",
				revisionRange, err, strings.TrimSpace(string(exitErr.Stderr)))
		}

		return nil, fmt.Errorf("reading the commits of %q: %w", revisionRange, err)
	}

	var messages []string

	for part := range strings.SplitSeq(string(out), "\x00") {
		if strings.TrimSpace(part) != "" {
			messages = append(messages, strings.TrimSpace(part))
		}
	}

	return messages, nil
}

// check validates every message and reports all the problems of all of them, rather than
// stopping at the first: rewriting a branch's history once is work, doing it three times
// because the check reported one commit at a time is worse.
func check(messages []string) error {
	failures := 0

	for _, message := range messages {
		problems := validate(message)
		if len(problems) == 0 {
			continue
		}

		failures++

		fmt.Fprintf(os.Stderr, "\n  %s\n", firstLine(message))

		for _, problem := range problems {
			fmt.Fprintf(os.Stderr, "    - %s\n", problem)
		}
	}

	if failures > 0 {
		fmt.Fprintf(os.Stderr, `
  Expected:  type(scope)!: subject

    types    %s
    scope    optional, lowercase
    !        optional, marks a breaking change
    subject  required, lowercase, no trailing period, header <= %d characters

  Examples:  feat(tours): add the variant manifest
             fix(pkg/amqp): reconnect after a channel error
             feat(proto)!: drop the deprecated SearchTours filter

`, strings.Join(types, " "), maxHeaderLength)

		fmt.Fprintf(os.Stderr, "commitcheck: %d %v\n\n", failures, errProblems)

		return errProblems
	}

	// An empty range passing reads exactly like a real pass, and that is how a check ends up
	// verifying nothing: a wrong range, no commits, green.
	if len(messages) == 0 {
		fmt.Println("==> commitcheck: no commits in the range, nothing was checked")

		return nil
	}

	fmt.Printf("==> commitcheck: %d message(s), all conventional\n", len(messages))

	return nil
}

// validate returns every rule the message breaks.
func validate(message string) []string {
	header := firstLine(message)

	// Merges and reverts are written by git and by GitHub, not by a person, and neither can
	// be made conventional without rewriting what the tool produced. They are the only
	// exemptions, and they are narrow on purpose.
	if strings.HasPrefix(header, "Merge ") || strings.HasPrefix(header, "Revert ") {
		return nil
	}

	var problems []string

	if header == "" {
		return []string{"the message is empty"}
	}

	if len(header) > maxHeaderLength {
		problems = append(problems,
			fmt.Sprintf("the header is %d characters, the limit is %d", len(header), maxHeaderLength))
	}

	match := headerPattern.FindStringSubmatch(header)
	if match == nil {
		problems = append(problems, `it does not match "type(scope): subject"`)

		return problems
	}

	commitType := match[headerPattern.SubexpIndex("type")]
	subject := match[headerPattern.SubexpIndex("subject")]

	if !slices.Contains(types, commitType) {
		problems = append(problems,
			fmt.Sprintf("%q is not one of the allowed types", commitType))
	}

	if strings.HasSuffix(subject, ".") {
		problems = append(problems, "the subject ends with a period")
	}

	if first := subject[:1]; first != strings.ToLower(first) {
		problems = append(problems, "the subject starts with a capital letter")
	}

	// A body has to be separated from the header by a blank line, or git and everything that
	// reads a subject treat the whole thing as one.
	lines := strings.Split(message, "\n")
	if len(lines) > 1 && strings.TrimSpace(lines[1]) != "" {
		problems = append(problems, "the second line is not blank, so the body runs into the header")
	}

	return problems
}

func firstLine(message string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(message), "\n")

	return strings.TrimSpace(line)
}
