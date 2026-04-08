package cli

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type ghRepoRef struct {
	Selector string
	Owner    string
}

type ghPRRemoteConfig struct {
	BaseRepoSelector string
	PushRepoSelector string
	HeadOwner        string
}

func (c ghPRRemoteConfig) HeadSelector(branch string) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return ""
	}
	if strings.TrimSpace(c.HeadOwner) == "" {
		return branch
	}
	return strings.TrimSpace(c.HeadOwner) + ":" + branch
}

func ghPRRemoteConfigFromRemoteURLs(fetchURL, pushURL string) (ghPRRemoteConfig, error) {
	baseRepo, err := ghRepoRefFromRemoteURL(fetchURL)
	if err != nil {
		return ghPRRemoteConfig{}, err
	}
	pushRepo, err := ghRepoRefFromRemoteURL(pushURL)
	if err != nil {
		return ghPRRemoteConfig{}, err
	}

	config := ghPRRemoteConfig{
		BaseRepoSelector: baseRepo.Selector,
		PushRepoSelector: pushRepo.Selector,
	}
	if pushRepo.Selector != baseRepo.Selector {
		config.HeadOwner = pushRepo.Owner
		if strings.TrimSpace(config.HeadOwner) == "" {
			return ghPRRemoteConfig{}, fmt.Errorf("push URL %q is missing a GitHub owner", pushURL)
		}
	}
	return config, nil
}

func ghRepoRefFromRemoteURL(remoteURL string) (ghRepoRef, error) {
	remoteURL = strings.TrimSpace(remoteURL)
	if remoteURL == "" {
		return ghRepoRef{}, errors.New("remote URL is empty")
	}

	host := ""
	path := ""
	switch {
	case strings.Contains(remoteURL, "://"):
		parsed, err := url.Parse(remoteURL)
		if err != nil {
			return ghRepoRef{}, fmt.Errorf("parse remote URL %q: %w", remoteURL, err)
		}
		host = strings.ToLower(strings.TrimSpace(parsed.Hostname()))
		path = parsed.Path
	case strings.Contains(remoteURL, ":") && !strings.Contains(strings.SplitN(remoteURL, ":", 2)[0], "/"):
		left, right, _ := strings.Cut(remoteURL, ":")
		host = left
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}
		host = strings.ToLower(strings.TrimSpace(host))
		path = right
	default:
		return ghRepoRef{}, fmt.Errorf("remote URL %q is not a GitHub repository URL", remoteURL)
	}

	path = strings.TrimSpace(strings.Trim(path, "/"))
	path = strings.TrimSuffix(path, ".git")
	if host == "" || path == "" {
		return ghRepoRef{}, fmt.Errorf("remote URL %q is not a GitHub repository URL", remoteURL)
	}
	segments := strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
	if len(segments) < 2 {
		return ghRepoRef{}, fmt.Errorf("remote URL %q is not a GitHub repository URL", remoteURL)
	}
	owner := strings.TrimSpace(segments[0])
	path = strings.Join(segments, "/")
	if host == "github.com" {
		return ghRepoRef{Selector: path, Owner: owner}, nil
	}
	return ghRepoRef{Selector: host + "/" + path, Owner: owner}, nil
}
