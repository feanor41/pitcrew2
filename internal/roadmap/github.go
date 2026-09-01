package roadmap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var ErrAlreadyBound = errors.New("roadmap item is already bound")

type PrepareInput struct {
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
}

type PreparedPublication struct {
	RoadmapID string `json:"roadmap_id"`
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Marker    string `json:"marker"`
	Digest    string `json:"digest"`
}

func (s *Service) PrepareGitHub(ctx context.Context, id string, input PrepareInput) (PreparedPublication, error) {
	if err := validateGitHubTarget(input.Provider, input.Namespace); err != nil {
		return PreparedPublication{}, err
	}
	item, err := s.Show(ctx, id)
	if err != nil {
		return PreparedPublication{}, err
	}
	if item.Binding != nil {
		return PreparedPublication{}, ErrAlreadyBound
	}
	return prepareGitHub(item, input.Provider, input.Namespace)
}

func prepareGitHub(item Item, provider, namespace string) (PreparedPublication, error) {
	marker := fmt.Sprintf("<!-- pitcrew-roadmap:v1 id=%s -->", item.ID)
	body := strings.TrimRight(item.Body, "\r\n") + "\n\n" + marker + "\n"
	digest, err := publicationDigest(provider, namespace, item.Title, body, marker)
	if err != nil {
		return PreparedPublication{}, err
	}
	return PreparedPublication{
		RoadmapID: item.ID,
		Provider:  provider,
		Namespace: namespace,
		Title:     item.Title,
		Body:      body,
		Marker:    marker,
		Digest:    digest,
	}, nil
}

func publicationDigest(provider, namespace, title, body, marker string) (string, error) {
	canonical := struct {
		Version   int    `json:"version"`
		Provider  string `json:"provider"`
		Namespace string `json:"namespace"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		Marker    string `json:"marker"`
	}{
		Version:   1,
		Provider:  provider,
		Namespace: namespace,
		Title:     title,
		Body:      body,
		Marker:    marker,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encode roadmap publication: %w", err)
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte("pitcrew-roadmap-publication\n"))
	_, _ = digest.Write(encoded)
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), nil
}

var (
	githubOwner = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?$`)
	githubRepo  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{0,98}[a-z0-9])?$`)
)

func validateGitHubTarget(provider, namespace string) error {
	if provider != "github" {
		return errors.New("provider must be github")
	}
	parts := strings.Split(namespace, "/")
	if len(parts) != 2 || !githubOwner.MatchString(parts[0]) || !githubRepo.MatchString(parts[1]) {
		return errors.New("namespace must be a normalized GitHub owner/repository")
	}
	return nil
}
