package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

type PR struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	IsDraft bool   `json:"isDraft"`
	Head    string `json:"headRefName"`
	Base    string `json:"baseRefName"`
}

type Check struct {
	Name  string
	State string // pass, fail, pending, skip
	URL   string
}

type Comment struct {
	Author  string
	Body    string
	URL     string
	Path    string // set for review (line) comments
	Line    int
	Replies int
}

type Status struct {
	Branch   string
	PR       *PR
	CI       string // rollup: SUCCESS, FAILURE, PENDING, ERROR, EXPECTED, or ""
	Checks   []Check
	Stack    []PR
	Threads  []Comment // unresolved review threads
	Resolved int
	Comments []Comment // conversation comments and review summaries
}

var errNoRepo = errors.New("not a git repository")

const prQuery = `query($owner: String!, $name: String!, $branch: String!) {
  repository(owner: $owner, name: $name) {
    defaultBranchRef { name }
    pullRequests(headRefName: $branch, states: OPEN, first: 1) {
      nodes {
        number title url isDraft headRefName baseRefName
        commits(last: 1) { nodes { commit { statusCheckRollup {
          state
          contexts(first: 100) { nodes {
            __typename
            ... on CheckRun { name status conclusion detailsUrl }
            ... on StatusContext { context state targetUrl }
          } }
        } } } }
        comments(last: 30) { nodes { author { login } body url } }
        reviews(last: 30) { nodes { author { login } body url } }
        reviewThreads(first: 100) { nodes {
          isResolved path line originalLine
          comments(first: 1) { totalCount nodes { author { login } body url } }
        } }
      }
    }
  }
}`

type author struct {
	Login string `json:"login"`
}

type commentNode struct {
	Author author `json:"author"`
	Body   string `json:"body"`
	URL    string `json:"url"`
}

type prResponse struct {
	Data struct {
		Repository struct {
			DefaultBranchRef struct {
				Name string `json:"name"`
			} `json:"defaultBranchRef"`
			PullRequests struct {
				Nodes []struct {
					PR
					Commits struct {
						Nodes []struct {
							Commit struct {
								StatusCheckRollup *struct {
									State    string `json:"state"`
									Contexts struct {
										Nodes []struct {
											Typename   string `json:"__typename"`
											Name       string `json:"name"`
											Status     string `json:"status"`
											Conclusion string `json:"conclusion"`
											DetailsURL string `json:"detailsUrl"`
											Context    string `json:"context"`
											State      string `json:"state"`
											TargetURL  string `json:"targetUrl"`
										} `json:"nodes"`
									} `json:"contexts"`
								} `json:"statusCheckRollup"`
							} `json:"commit"`
						} `json:"nodes"`
					} `json:"commits"`
					Comments struct {
						Nodes []commentNode `json:"nodes"`
					} `json:"comments"`
					Reviews struct {
						Nodes []commentNode `json:"nodes"`
					} `json:"reviews"`
					ReviewThreads struct {
						Nodes []struct {
							IsResolved   bool   `json:"isResolved"`
							Path         string `json:"path"`
							Line         int    `json:"line"`
							OriginalLine int    `json:"originalLine"`
							Comments     struct {
								TotalCount int           `json:"totalCount"`
								Nodes      []commentNode `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	} `json:"data"`
}

func run(dir, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("%s: %s", name, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

func currentBranch(dir string) (string, error) {
	out, err := run(dir, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", errNoRepo
	}
	return strings.TrimSpace(string(out)), nil
}

// fetchStatus loads the open PR for dir's current branch. {owner}/{repo} are
// resolved by gh from dir's remotes, and gh's own auth is reused.
func fetchStatus(dir string) (*Status, error) {
	branch, err := currentBranch(dir)
	if err != nil {
		return nil, err
	}
	st := &Status{Branch: branch}
	if branch == "HEAD" {
		return st, nil
	}

	out, err := run(dir, "gh", "api", "graphql",
		"-F", "owner={owner}", "-F", "name={repo}", "-F", "branch="+branch, "-f", "query="+prQuery)
	if err != nil {
		return nil, err
	}
	var resp prResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, err
	}
	repo := resp.Data.Repository
	if len(repo.PullRequests.Nodes) == 0 {
		return st, nil
	}
	n := repo.PullRequests.Nodes[0]
	st.PR = &n.PR

	if len(n.Commits.Nodes) > 0 && n.Commits.Nodes[0].Commit.StatusCheckRollup != nil {
		roll := n.Commits.Nodes[0].Commit.StatusCheckRollup
		st.CI = roll.State
		for _, c := range roll.Contexts.Nodes {
			if c.Typename == "StatusContext" {
				st.Checks = append(st.Checks, Check{c.Context, contextState(c.State), c.TargetURL})
			} else {
				st.Checks = append(st.Checks, Check{c.Name, checkRunState(c.Status, c.Conclusion), c.DetailsURL})
			}
		}
	}

	for _, t := range n.ReviewThreads.Nodes {
		if t.IsResolved {
			st.Resolved++
			continue
		}
		if len(t.Comments.Nodes) == 0 {
			continue
		}
		c := t.Comments.Nodes[0]
		line := t.Line
		if line == 0 {
			line = t.OriginalLine // outdated thread
		}
		st.Threads = append(st.Threads, Comment{c.Author.Login, cleanBody(c.Body), c.URL, t.Path, line, t.Comments.TotalCount - 1})
	}
	for _, c := range append(n.Reviews.Nodes, n.Comments.Nodes...) {
		if body := cleanBody(c.Body); body != "" {
			st.Comments = append(st.Comments, Comment{Author: c.Author.Login, Body: body, URL: c.URL})
		}
	}

	// The stack walk needs every open PR; a failure here just hides the stack.
	out, err = run(dir, "gh", "pr", "list", "--state", "open", "--limit", "1000",
		"--json", "number,title,url,isDraft,headRefName,baseRefName")
	if err == nil {
		var open []PR
		if json.Unmarshal(out, &open) == nil {
			st.Stack = stackOf(*st.PR, open, repo.DefaultBranchRef.Name)
		}
	}
	return st, nil
}

func checkRunState(status, conclusion string) string {
	if status != "COMPLETED" {
		return "pending"
	}
	switch conclusion {
	case "SUCCESS", "NEUTRAL":
		return "pass"
	case "SKIPPED", "STALE":
		return "skip"
	}
	return "fail"
}

func contextState(state string) string {
	switch state {
	case "SUCCESS":
		return "pass"
	case "PENDING", "EXPECTED":
		return "pending"
	}
	return "fail"
}

var (
	htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	htmlTag     = regexp.MustCompile(`<[^>]+>`)
	mdImage     = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	mdLink      = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	mdMarks     = regexp.MustCompile("^[#>*\\-\\s]+|[*`]+")
)

// cleanBody turns a markdown comment into plain text lines for a narrow panel.
func cleanBody(s string) string {
	s = htmlComment.ReplaceAllString(s, "")
	s = mdImage.ReplaceAllString(s, "")
	s = mdLink.ReplaceAllString(s, "$1")
	s = htmlTag.ReplaceAllString(s, "")
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(mdMarks.ReplaceAllString(l, ""))
		if l != "" && !strings.HasPrefix(l, "|") {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}
