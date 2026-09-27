package github

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func repoPath(owner, name string) string {
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
}

// escapePath escapes each segment of a repository path.
func escapePath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// FileContent returns a file of a repository at ref (a branch, tag or
// commit; empty for the default branch). Files over 1 MB are not
// supported by the API.
func (c *Client) FileContent(ctx context.Context, owner, name, path, ref string) ([]byte, error) {
	var out struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	u := repoPath(owner, name) + "/contents/" + escapePath(path)
	if ref != "" {
		u += "?ref=" + url.QueryEscape(ref)
	}
	if err := c.do(ctx, http.MethodGet, u, nil, &out); err != nil {
		return nil, err
	}
	if out.Type != "file" || out.Encoding != "base64" {
		return nil, &Error{Status: http.StatusNotFound, Message: path + " is not a file"}
	}
	return base64.StdEncoding.DecodeString(strings.ReplaceAll(out.Content, "\n", ""))
}

// Tree lists the files of a repository at ref. Truncated reports that the
// repository is too large to list at once (over 100,000 entries).
func (c *Client) Tree(ctx context.Context, owner, name, ref string) (files []string, truncated bool, err error) {
	var out struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := c.do(ctx, http.MethodGet, repoPath(owner, name)+"/git/trees/"+url.PathEscape(ref)+"?recursive=1", nil, &out); err != nil {
		return nil, false, err
	}
	for _, e := range out.Tree {
		if e.Type == "blob" {
			files = append(files, e.Path)
		}
	}
	return files, out.Truncated, nil
}

// Commit is a commit as git blame reports it.
type Commit struct {
	SHA         string
	Message     string // first line
	URL         string
	AuthorName  string
	AuthorEmail string
	AuthorLogin string // GitHub user of the author, when known
	CommittedAt time.Time
}

// BlameRange is a run of lines (1-based, inclusive) last changed by one
// commit.
type BlameRange struct {
	Start, End int
	Commit     Commit
}

// graphqlURL is the GraphQL endpoint next to a REST API URL
// ("https://api.github.com" or GitHub Enterprise's "https://host/api/v3").
func graphqlURL(api string) string {
	if strings.HasSuffix(api, "/api/v3") {
		return strings.TrimSuffix(api, "/v3") + "/graphql"
	}
	return api + "/graphql"
}

const blameQuery = `query($owner: String!, $name: String!, $ref: String!, $path: String!) {
  repository(owner: $owner, name: $name) {
    object(expression: $ref) {
      ... on Commit {
        blame(path: $path) {
          ranges {
            startingLine
            endingLine
            commit { oid messageHeadline url committedDate author { name email user { login } } }
          }
        }
      }
    }
  }
}`

// Blame returns who last changed each line of a file at ref, through the
// GraphQL API (REST has no blame). A path that does not exist at ref is a
// not-found Error.
func (c *Client) Blame(ctx context.Context, owner, name, ref, path string) ([]BlameRange, error) {
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data struct {
			Repository *struct {
				Object *struct {
					Blame *struct {
						Ranges []struct {
							StartingLine int `json:"startingLine"`
							EndingLine   int `json:"endingLine"`
							Commit       struct {
								OID             string    `json:"oid"`
								MessageHeadline string    `json:"messageHeadline"`
								URL             string    `json:"url"`
								CommittedDate   time.Time `json:"committedDate"`
								Author          struct {
									Name  string `json:"name"`
									Email string `json:"email"`
									User  *struct {
										Login string `json:"login"`
									} `json:"user"`
								} `json:"author"`
							} `json:"commit"`
						} `json:"ranges"`
					} `json:"blame"`
				} `json:"object"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	body := map[string]any{"query": blameQuery, "variables": map[string]string{"owner": owner, "name": name, "ref": ref, "path": strings.Trim(path, "/")}}
	if err := request(ctx, c.http, http.MethodPost, graphqlURL(c.api), "bearer "+token, body, &out); err != nil {
		return nil, err
	}
	repo := out.Data.Repository
	if repo == nil || repo.Object == nil || repo.Object.Blame == nil {
		msg := "file not found"
		if len(out.Errors) > 0 {
			msg = out.Errors[0].Message
		}
		return nil, &Error{Status: http.StatusNotFound, Message: msg}
	}
	ranges := make([]BlameRange, 0, len(repo.Object.Blame.Ranges))
	for _, r := range repo.Object.Blame.Ranges {
		cm := Commit{SHA: r.Commit.OID, Message: r.Commit.MessageHeadline, URL: r.Commit.URL, AuthorName: r.Commit.Author.Name,
			AuthorEmail: r.Commit.Author.Email, CommittedAt: r.Commit.CommittedDate}
		if r.Commit.Author.User != nil {
			cm.AuthorLogin = r.Commit.Author.User.Login
		}
		ranges = append(ranges, BlameRange{Start: r.StartingLine, End: r.EndingLine, Commit: cm})
	}
	return ranges, nil
}
