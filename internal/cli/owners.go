package cli

import (
	"sort"
	"strings"

	"github.com/walm/todomd/internal/cloud"
	"github.com/walm/todomd/internal/cloudsync"
)

// projectRef is the project object in JSON output.
func projectRef(l *cloudsync.Link) map[string]any {
	out := map[string]any{"id": l.ProjectID, "slug": l.ProjectSlug, "url": l.ProjectURL}
	if l.Owner != "" {
		out["owner"] = l.Owner
	}
	if l.OwnerName != "" {
		out["owner_name"] = l.OwnerName
	}
	return out
}

// ownerNote is " (workspace Acme)" for a workspace's project, else "".
func ownerNote(p cloud.Project) string {
	if !p.InWorkspace() {
		return ""
	}
	name := p.OwnerName
	if name == "" {
		name = p.OwnerHandle()
	}
	return " (workspace " + name + ")"
}

// projectChoices builds the link picker: projects for this repo first, then
// "Create a new project", then every project grouped as the web does:
// Personal, each workspace (by name), then shared with you (other people's).
// headings[i] is printed above option i; picks[i] is nil for "Create a
// new project".
func projectChoices(all, matches []cloud.Project, myHandle string) (options []string, picks []*cloud.Project, headings map[int]string) {
	seen := map[string]bool{}
	headings = map[int]string{}
	for i := range matches {
		options = append(options, matches[i].Slug+"  (this repo)")
		picks = append(picks, &matches[i])
		seen[matches[i].ID] = true
	}
	options = append(options, "Create a new project")
	picks = append(picks, nil)

	var personal, shared []int
	workspaces := map[string][]int{}
	var order []string // workspace handles
	for i := range all {
		p := all[i]
		if seen[p.ID] {
			continue
		}
		switch {
		case p.InWorkspace():
			h := p.OwnerHandle()
			if _, ok := workspaces[h]; !ok {
				order = append(order, h)
			}
			workspaces[h] = append(workspaces[h], i)
		case strings.EqualFold(p.OwnerHandle(), myHandle):
			personal = append(personal, i)
		default:
			shared = append(shared, i)
		}
	}
	sort.Slice(order, func(a, b int) bool {
		na, nb := wsName(all, workspaces[order[a]][0]), wsName(all, workspaces[order[b]][0])
		if !strings.EqualFold(na, nb) {
			return strings.ToLower(na) < strings.ToLower(nb)
		}
		return order[a] < order[b]
	})
	add := func(heading string, idx []int) {
		if len(idx) == 0 {
			return
		}
		headings[len(options)] = heading
		for _, i := range idx {
			options = append(options, all[i].Slug)
			picks = append(picks, &all[i])
		}
	}
	add("Personal", personal)
	for _, h := range order {
		add(wsName(all, workspaces[h][0]), workspaces[h])
	}
	add("shared with you", shared)
	return options, picks, headings
}

func wsName(all []cloud.Project, i int) string {
	if all[i].OwnerName != "" {
		return all[i].OwnerName
	}
	return all[i].OwnerHandle()
}
