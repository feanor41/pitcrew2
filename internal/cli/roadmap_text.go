package cli

import (
	"fmt"
	"strings"

	"github.com/fmazzalomo/pitcrew/internal/roadmap"
)

func renderRoadmapItem(item roadmap.Item) string {
	var output strings.Builder
	fmt.Fprintf(&output, "ROADMAP ITEM\nID: %s\nTitle: %q\nBody: %q\nProvenance: %s\nCreated: %s\nLifecycle: %s\nBinding: %s\nAuthority: %s\n", item.ID, item.Title, item.Body, item.Provenance, item.CreatedAt, item.LocalLifecycle, item.BindingState, item.Authority)
	if item.Binding != nil {
		fmt.Fprintf(&output, "Provider: %s\nNamespace: %s\nExternal ID: %s\nURL: %s\nPrepared digest: %s\nAcknowledged: %s\n", item.Binding.Provider, item.Binding.Namespace, item.Binding.ExternalID, item.Binding.URL, item.Binding.PreparedDigest, item.Binding.AcknowledgedAt)
	}
	return output.String()
}

func renderRoadmapList(items []roadmap.Summary) string {
	var output strings.Builder
	output.WriteString("ROADMAP ITEMS\n")
	if len(items) == 0 {
		output.WriteString("No roadmap items.\n")
	}
	for _, item := range items {
		fmt.Fprintf(&output, "%s | %s | %s | %s | %q\n", item.ID, item.CreatedAt, item.Authority, item.BindingState, item.Title)
	}
	return output.String()
}

func renderRoadmapPublication(publication roadmap.PreparedPublication) string {
	return fmt.Sprintf("GITHUB PUBLICATION\nRoadmap: %s\nProvider: %s\nNamespace: %s\nTitle: %q\nBody: %q\nMarker: %s\nDigest: %s\n", publication.RoadmapID, publication.Provider, publication.Namespace, publication.Title, publication.Body, publication.Marker, publication.Digest)
}
