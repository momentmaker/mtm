package browsers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// chromiumExtension is the form of a Chromium extension id; the
// Extensions folder also holds working folders such as Temp.
var chromiumExtension = regexp.MustCompile(`^[a-p]{32}$`)

// chromiumProfiles reads the profile list in Local State, which maps each
// profile's folder to the name the browser shows. Only the name is read:
// Local State also holds account details mtm has no use for. A browser
// with no Local State has never run, or left only leftovers, and has no
// profiles.
func chromiumProfiles(root string) ([]Profile, error) {
	path := filepath.Join(root, "Local State")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var state struct {
		Profile struct {
			InfoCache map[string]struct {
				Name string `json:"name"`
			} `json:"info_cache"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var out []Profile
	for _, dir := range slices.Sorted(maps.Keys(state.Profile.InfoCache)) {
		if !isFolderName(dir) {
			continue
		}
		folder := filepath.Join(root, dir)
		if _, err := os.Stat(folder); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		exts, err := chromiumExtensions(filepath.Join(folder, "Extensions"))
		if err != nil {
			return nil, err
		}
		out = append(out, Profile{Dir: dir, Name: state.Profile.InfoCache[dir].Name, Extensions: exts})
	}
	return out, nil
}

// isFolderName rejects a profile key that isn't one folder inside the
// browser's, so an edited Local State can't send mtm looking elsewhere.
func isFolderName(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsRune(s, '/')
}

func chromiumExtensions(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && chromiumExtension.MatchString(e.Name()) {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

// firefoxProfiles reads profiles.ini. Each [Profile<n>] section has the
// profile's Name and Path, which is relative to the Firefox folder when
// IsRelative=1.
func firefoxProfiles(root string) ([]Profile, error) {
	path := filepath.Join(root, "profiles.ini")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	sections, err := iniSections(data)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var out []Profile
	for _, sec := range sections {
		if !strings.HasPrefix(sec.name, "Profile") || sec.keys["Path"] == "" {
			continue
		}
		p := Profile{Dir: sec.keys["Path"], Name: sec.keys["Name"]}
		folder := p.Dir
		if sec.keys["IsRelative"] == "1" {
			if !isRelativeFolder(p.Dir) {
				continue
			}
			folder = filepath.Join(root, filepath.FromSlash(p.Dir))
		}
		if _, err := os.Stat(folder); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if p.Extensions, err = firefoxExtensions(folder); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// isRelativeFolder rejects a relative profile path that climbs out of the
// Firefox folder.
func isRelativeFolder(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") {
		return false
	}
	return !slices.Contains(strings.Split(p, "/"), "..")
}

// firefoxExtensions reads the ids of the extensions the user installed
// from extensions.json. Firefox lists its built-in ones there too, under
// other locations.
func firefoxExtensions(profile string) ([]string, error) {
	path := filepath.Join(profile, "extensions.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var doc struct {
		Addons []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Location string `json:"location"`
		} `json:"addons"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var ids []string
	for _, a := range doc.Addons {
		if a.Type == "extension" && a.Location == "app-profile" && a.ID != "" {
			ids = append(ids, a.ID)
		}
	}
	slices.Sort(ids)
	return ids, nil
}

type iniSection struct {
	name string
	keys map[string]string
}

// iniSections parses the simple INI Firefox writes: [Section] headers and
// Key=Value lines.
func iniSections(data []byte) ([]iniSection, error) {
	var out []iniSection
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			out = append(out, iniSection{name: line[1 : len(line)-1], keys: map[string]string{}})
		case len(out) > 0:
			if k, v, ok := strings.Cut(line, "="); ok {
				out[len(out)-1].keys[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
	}
	return out, sc.Err()
}
