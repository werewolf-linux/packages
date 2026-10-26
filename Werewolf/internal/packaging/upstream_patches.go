package packaging

import (
	"fmt"
	"path/filepath"
)

// upstreamPatch keeps the capture unchanged and requires review if Wolfi edits
// a patch whose context we refreshed. Build and prepare use the same replacement.
func upstreamPatch(pkg Package, captured, name string) (string, error) {
	original, err := patchFile(captured, name)
	if err != nil {
		return "", err
	}
	refresh, ok := pkg.Spec.UpstreamPatches[name]
	if !ok {
		return original, nil
	}
	sum, err := digest(original)
	if err != nil {
		return "", err
	}
	if sum != refresh.SHA256 {
		return "", fmt.Errorf("review changed upstream patch %s: SHA-256 differs from the refreshed baseline", name)
	}
	return patchFile(pkg.Directory, refresh.Source)
}

func patchFile(directory, name string) (string, error) {
	if !filepath.IsLocal(name) {
		return "", fmt.Errorf("unsafe upstream patch path: %s", name)
	}
	root, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", err
	}
	path, err := filepath.EvalSymlinks(filepath.Join(root, name))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(relative) {
		return "", fmt.Errorf("upstream patch escapes its directory: %s", name)
	}
	return path, nil
}

func copyUpstreamPatches(pkg Package, captured, destination string) error {
	for name := range pkg.Spec.UpstreamPatches {
		path, err := upstreamPatch(pkg, captured, name)
		if err != nil {
			return err
		}
		if destination != "" {
			if err := copyFile(path, filepath.Join(destination, name)); err != nil {
				return err
			}
		}
	}
	return nil
}
