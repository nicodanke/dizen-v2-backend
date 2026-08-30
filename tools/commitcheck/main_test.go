package main

import (
	"strings"
	"testing"
)

// The checker exists to reject things. A linter with no tests is the kind of code that
// silently accepts everything after one bad edit to a regular expression, and nobody
// notices because green is what it always was -- so the rejections are what is tested here,
// one case per rule.
func TestValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		message string
		problem string // a fragment of the expected problem; empty means the message is valid
	}{
		{
			name:    "plain type and subject",
			message: "feat: add the variant manifest",
		},
		{
			name:    "with a scope",
			message: "fix(tours): reject a variant without a route",
		},
		{
			name:    "a scope with a path",
			message: "refactor(pkg/amqp): extract the retry policy",
		},
		{
			name:    "breaking change",
			message: "feat(proto)!: drop the deprecated SearchTours filter",
		},
		{
			name:    "with a body separated by a blank line",
			message: "fix(booking): release the slot on cancellation\n\nThe slot stayed held.",
		},
		{
			name:    "a trailer the contract publication reads",
			message: "feat(proto): add the elevation field\n\napi-release: patch",
		},
		{
			name:    "a merge commit is not ours to format",
			message: "Merge pull request #12 from nicodanke/feature/tours",
		},
		{
			name:    "a revert is written by git",
			message: `Revert "feat: add the variant manifest"`,
		},
		{
			name:    "no type at all",
			message: "add the variant manifest",
			problem: "does not match",
		},
		{
			name:    "an invented type",
			message: "chores: tidy the modules",
			problem: "not one of the allowed types",
		},
		{
			name:    "no space after the colon",
			message: "feat:add the variant manifest",
			problem: "does not match",
		},
		{
			name:    "an empty subject",
			message: "feat: ",
			problem: "does not match",
		},
		{
			name:    "a capitalized subject",
			message: "feat: Add the variant manifest",
			problem: "capital letter",
		},
		{
			name:    "a trailing period",
			message: "feat: add the variant manifest.",
			problem: "ends with a period",
		},
		{
			name:    "an uppercase scope",
			message: "feat(Tours): add the variant manifest",
			problem: "does not match",
		},
		{
			name:    "a header past the limit",
			message: "feat(tours): " + strings.Repeat("a", maxHeaderLength),
			problem: "the limit is",
		},
		{
			name:    "a body glued to the header",
			message: "feat: add the variant manifest\nThe manifest carries the nodes.",
			problem: "second line is not blank",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			problems := validate(testCase.message)

			if testCase.problem == "" {
				if len(problems) != 0 {
					t.Fatalf("expected no problem, got %v", problems)
				}

				return
			}

			if len(problems) == 0 {
				t.Fatalf("expected a problem containing %q, got none", testCase.problem)
			}

			for _, problem := range problems {
				if strings.Contains(problem, testCase.problem) {
					return
				}
			}

			t.Fatalf("expected a problem containing %q, got %v", testCase.problem, problems)
		})
	}
}

// Every commit already in this repository has to pass, or the check is one that could never
// have been introduced.
func TestHistoryOfThisRepositoryPasses(t *testing.T) {
	t.Parallel()

	for _, message := range []string{
		"feat: first commit with project structure",
		"chore: remove yaak env info",
		"feat: add CI/CD scripts",
	} {
		if problems := validate(message); len(problems) != 0 {
			t.Errorf("%q should be valid, got %v", message, problems)
		}
	}
}
