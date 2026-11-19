package configview

import (
	"path/filepath"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
)

// archive is what the projects page knows of the archive: the shared
// project.Archive (raw project keys with their derived names) and whether
// it was loaded.
type archive struct {
	project.Archive
	loaded bool
	err    error
}

// newArchive names the keys as the stats view would without definitions.
func newArchive(keys []store.ProjectKey, home string) archive {
	return archive{Archive: project.NewArchive(keys, home), loaded: true}
}

// suggest is the definition a key of the archive would get: the derived
// name (the base name for a directory) and the remote URL as the pattern,
// else the path with "~"; the remote URL covers every worktree and
// checkout of the repository.
func suggest(k store.ProjectKey, a archive, home string) config.Project {
	name := a.NameOf(k.Key)
	if k.Dir || name == "" {
		name = filepath.Base(k.Key)
	}
	return config.Project{Name: name, Match: config.StringList{suggestPattern(k, home)}}
}

// suggestPattern is the pattern that names a key best: the remote URL of a
// repository, else the path with "~".
func suggestPattern(k store.ProjectKey, home string) string {
	if k.Remote != "" {
		return k.Remote
	}
	return project.Tilde(k.Key, home)
}
