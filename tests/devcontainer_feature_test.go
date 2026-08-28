package tests_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The dev container feature in src/ installs a *release binary*, so it depends
// on facts that live in .goreleaser.yaml: which repository the release is cut
// from, what the linux assets are called, and the name of the checksum
// manifest. Nothing at build time connects the two - a rename in the release
// config would leave install.sh downloading a URL that 404s, and the first
// person to notice would be a consumer whose container stopped building.
//
// These tests are that connection, in the same spirit as devcontainer_test.go:
// whoever changes one side, CI stays red until the other agrees.

const (
	featuresDir        = "src"
	featureTestsDir    = "test"
	featureMetadata    = "devcontainer-feature.json"
	featureInstallPath = "src/mdm/install.sh"
	featureReadmePath  = "src/mdm/README.md"
	featureTestWfPath  = ".github/workflows/devcontainer-feature.yml"
	featureReleaseWf   = ".github/workflows/devcontainer-feature-release.yml"
)

type featureOption struct {
	Type        string `json:"type"`
	Default     any    `json:"default"`
	Description string `json:"description"`
	Proposals   []any  `json:"proposals"`
}

type featureMeta struct {
	ID          string                   `json:"id"`
	Version     string                   `json:"version"`
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Options     map[string]featureOption `json:"options"`
}

// featureIDs returns every feature directory under src/, keyed by its declared
// id. The directory name and the id have to agree - the devcontainer CLI
// addresses features by directory, the registry by id.
func featureIDs(t *testing.T) map[string]featureMeta {
	t.Helper()

	root, err := findModRoot()
	if err != nil {
		t.Fatalf("could not locate module root: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(root, featuresDir))
	if err != nil {
		t.Fatalf("read %s: %v", featuresDir, err)
	}

	out := make(map[string]featureMeta)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(featuresDir, e.Name(), featureMetadata))
		var meta featureMeta
		if err := json.Unmarshal([]byte(readRepoFile(t, rel)), &meta); err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}
		if meta.ID != e.Name() {
			t.Errorf("%s declares id %q but lives in %s/%s; the devcontainer CLI "+
				"addresses features by directory name and the registry by id, so they must match",
				rel, meta.ID, featuresDir, e.Name())
		}
		out[meta.ID] = meta
	}

	if len(out) == 0 {
		t.Fatalf("no features found under %s/", featuresDir)
	}
	return out
}

var semverPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func TestFeatureMetadata(t *testing.T) {
	for id, meta := range featureIDs(t) {
		if !semverPattern.MatchString(meta.Version) {
			t.Errorf("feature %s: version %q is not major.minor.patch; the registry "+
				"derives the :1 and :1.0 tags from it", id, meta.Version)
		}
		if meta.Name == "" || meta.Description == "" {
			t.Errorf("feature %s: name and description are what consumers see in the "+
				"feature index; both must be set", id)
		}

		opt, ok := meta.Options["version"]
		if !ok {
			t.Fatalf("feature %s: no `version` option - consumers cannot pin a release", id)
		}
		if opt.Type != "string" {
			t.Errorf("feature %s: `version` option is %q, want string", id, opt.Type)
		}
		if opt.Default != "latest" {
			t.Errorf("feature %s: `version` defaults to %v, want \"latest\"", id, opt.Default)
		}
	}
}

// --- agreement with .goreleaser.yaml ----------------------------------------

type goreleaserConfig struct {
	Builds []struct {
		Goos   []string `yaml:"goos"`
		Goarch []string `yaml:"goarch"`
	} `yaml:"builds"`
	Archives []struct {
		Formats      []string `yaml:"formats"`
		NameTemplate string   `yaml:"name_template"`
	} `yaml:"archives"`
	Checksum struct {
		NameTemplate string `yaml:"name_template"`
	} `yaml:"checksum"`
	Release struct {
		GitHub struct {
			Owner string `yaml:"owner"`
			Name  string `yaml:"name"`
		} `yaml:"github"`
	} `yaml:"release"`
}

func goreleaser(t *testing.T) goreleaserConfig {
	t.Helper()
	var cfg goreleaserConfig
	if err := yaml.Unmarshal([]byte(readRepoFile(t, ".goreleaser.yaml")), &cfg); err != nil {
		t.Fatalf("parse .goreleaser.yaml: %v", err)
	}
	if len(cfg.Builds) == 0 || len(cfg.Archives) == 0 {
		t.Fatalf(".goreleaser.yaml has no builds or no archives")
	}
	return cfg
}

// shellAssign pulls `NAME="value"` out of install.sh.
func shellAssign(t *testing.T, script, name string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `="([^"]*)"`)
	m := re.FindStringSubmatch(script)
	if m == nil {
		t.Fatalf("%s: no %s= assignment found", featureInstallPath, name)
	}
	return m[1]
}

func TestFeatureInstallTargetsTheReleaseArtifacts(t *testing.T) {
	cfg := goreleaser(t)
	script := readRepoFile(t, featureInstallPath)

	wantRepo := cfg.Release.GitHub.Owner + "/" + cfg.Release.GitHub.Name
	if got := shellAssign(t, script, "REPO"); got != wantRepo {
		t.Errorf("%s downloads from %q but .goreleaser.yaml publishes to %q",
			featureInstallPath, got, wantRepo)
	}

	if got := shellAssign(t, script, "CHECKSUM_FILE"); got != cfg.Checksum.NameTemplate {
		t.Errorf("%s verifies against %q but .goreleaser.yaml names the manifest %q",
			featureInstallPath, got, cfg.Checksum.NameTemplate)
	}

	// The feature downloads the asset and chmods it. That only works because
	// the release artifacts are bare binaries rather than archives.
	if formats := cfg.Archives[0].Formats; len(formats) != 1 || formats[0] != "binary" {
		t.Errorf(".goreleaser.yaml publishes archive formats %v; %s expects a bare "+
			"binary asset it can install directly", formats, featureInstallPath)
	}

	// Asset names come out of a GoReleaser template, so they cannot be derived
	// here without reimplementing it. Pinning the template instead means a
	// rename shows up as a failure right next to the file that has to change.
	const knownTemplate = `mdm-{{ if eq .Os "darwin" }}macos{{ else }}{{ .Os }}{{ end }}-{{ if eq .Arch "amd64" }}x64{{ else }}{{ .Arch }}{{ end }}`
	if cfg.Archives[0].NameTemplate != knownTemplate {
		t.Errorf(".goreleaser.yaml archive name_template changed:\n\tgot  %s\n\twant %s\n"+
			"the linux asset names in %s are built from it and must be updated to match",
			cfg.Archives[0].NameTemplate, knownTemplate, featureInstallPath)
	}

	// Under that template, linux/amd64 and linux/arm64 land on these names.
	for _, asset := range []string{"mdm-linux-x64", "mdm-linux-arm64"} {
		arch := strings.TrimPrefix(asset, "mdm-linux-")
		if !strings.Contains(script, `ARCH="`+arch+`"`) {
			t.Errorf("%s never selects ARCH=%q, so it can never download %s",
				featureInstallPath, arch, asset)
		}
	}
	if !strings.Contains(script, `ASSET="mdm-linux-${ARCH}"`) {
		t.Errorf(`%s no longer builds the asset name as mdm-linux-${ARCH}`, featureInstallPath)
	}

	// Both linux architectures have to actually be built, or one of the two
	// arch branches downloads a URL that does not exist.
	if !contains(cfg.Builds[0].Goos, "linux") {
		t.Errorf(".goreleaser.yaml no longer builds for linux")
	}
	for _, arch := range []string{"amd64", "arm64"} {
		if !contains(cfg.Builds[0].Goarch, arch) {
			t.Errorf(".goreleaser.yaml no longer builds linux/%s, which %s installs",
				arch, featureInstallPath)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// TestFeatureInstallsToASharedPath locks in the install location. A home
// directory would work for whichever user the build happened to run as and
// silently not for the remoteUser; /usr/local/bin is on PATH for everyone.
func TestFeatureInstallsToASharedPath(t *testing.T) {
	script := readRepoFile(t, featureInstallPath)
	if got := shellAssign(t, script, "INSTALL_DIR"); got != "/usr/local/bin" {
		t.Errorf("%s installs to %q, want /usr/local/bin so the binary is on PATH "+
			"for every user in the container", featureInstallPath, got)
	}

	if !strings.Contains(readRepoFile(t, "test/mdm/test.sh"), "/usr/local/bin/mdm") {
		t.Errorf("test/mdm/test.sh no longer asserts the install location")
	}
}

// TestFeatureBinaryIsOwnedByTheRemoteUser locks in the other half of the
// install location. /usr/local/bin holds a symlink; the binary itself sits in
// its own directory, chowned to _REMOTE_USER.
//
// That indirection exists for one reason: replacing a running executable means
// unlinking and recreating it, which is a permission on the directory. Put the
// binary straight into a root-owned /usr/local/bin and `mdm upgrade` fails for
// the remoteUser with "permission denied", leaving an image rebuild as the only
// route to a new release. Nothing in a container build fails when this
// regresses - the feature installs fine and only upgrades break, months later
// and in someone else's container.
func TestFeatureBinaryIsOwnedByTheRemoteUser(t *testing.T) {
	script := readRepoFile(t, featureInstallPath)

	binaryDir := shellAssign(t, script, "BINARY_DIR")
	if binaryDir == shellAssign(t, script, "INSTALL_DIR") {
		t.Errorf("%s puts the binary in %s itself; it belongs in its own "+
			"directory so the remoteUser can own it without owning every "+
			"other feature's binaries", featureInstallPath, binaryDir)
	}

	if !strings.Contains(script, `chown -R "${OWNER}:$(id -gn "$OWNER")" "$BINARY_DIR"`) {
		t.Errorf("%s no longer hands %s to the remote user, so `mdm upgrade` "+
			"inside the container is back to permission denied",
			featureInstallPath, binaryDir)
	}
	if !strings.Contains(script, `OWNER="${_REMOTE_USER:-}"`) {
		t.Errorf("%s no longer reads _REMOTE_USER, the only thing that names "+
			"the user the container will run as", featureInstallPath)
	}
	if !strings.Contains(script, `ln -sfn "${BINARY_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"`) {
		t.Errorf("%s no longer links the binary onto PATH", featureInstallPath)
	}

	// The scenario that would catch a regression at container-build time.
	nonRoot := readRepoFile(t, "test/mdm/non_root_user.sh")
	if !strings.Contains(nonRoot, "non-root user can replace the binary") {
		t.Error("test/mdm/non_root_user.sh no longer exercises replacing the " +
			"binary as the remote user, which is what `mdm upgrade` does")
	}
}

// --- tests, scenarios, and the scripts behind them --------------------------

// TestEveryFeatureHasTests keeps a new feature from shipping untested: the
// devcontainer CLI silently passes a feature with no test.sh.
func TestEveryFeatureHasTests(t *testing.T) {
	root, err := findModRoot()
	if err != nil {
		t.Fatalf("could not locate module root: %v", err)
	}

	for id := range featureIDs(t) {
		path := filepath.Join(root, featureTestsDir, id, "test.sh")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("feature %s has no %s/%s/test.sh", id, featureTestsDir, id)
		}
	}
}

// TestScenariosMatchTheirScripts checks both directions: every scenario in
// scenarios.json has the script the CLI will look for, and every script in the
// directory is reachable from a scenario. An orphan .sh file is a test that
// never runs, which is worse than no test at all.
func TestScenariosMatchTheirScripts(t *testing.T) {
	root, err := findModRoot()
	if err != nil {
		t.Fatalf("could not locate module root: %v", err)
	}
	features := featureIDs(t)

	for id := range features {
		dir := filepath.Join(root, featureTestsDir, id)
		rel := filepath.ToSlash(filepath.Join(featureTestsDir, id, "scenarios.json"))

		var scenarios map[string]struct {
			Image    string                     `json:"image"`
			Features map[string]json.RawMessage `json:"features"`
		}
		if err := json.Unmarshal([]byte(readRepoFile(t, rel)), &scenarios); err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}

		scripted := map[string]bool{"test.sh": true}
		for name, sc := range scenarios {
			scripted[name+".sh"] = true

			if sc.Image == "" {
				t.Errorf("%s: scenario %q has no image", rel, name)
			}
			for fid := range sc.Features {
				if _, ok := features[fid]; !ok {
					t.Errorf("%s: scenario %q installs unknown feature %q", rel, name, fid)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, name+".sh")); err != nil {
				t.Errorf("%s: scenario %q has no %s.sh; the CLI would run it with no assertions",
					rel, name, name)
			}
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			if filepath.Ext(e.Name()) != ".sh" {
				continue
			}
			if !scripted[e.Name()] {
				t.Errorf("%s/%s is not named by any scenario in %s, so it never runs",
					featureTestsDir, e.Name(), rel)
			}
		}
	}
}

// TestPinnedScenariosAssertTheirVersion catches the copy-paste failure a
// version-pinning test is most prone to: bumping the pin in scenarios.json and
// leaving the assertion checking for the old release, which then passes whether
// or not the option works.
func TestPinnedScenariosAssertTheirVersion(t *testing.T) {
	for id := range featureIDs(t) {
		rel := filepath.ToSlash(filepath.Join(featureTestsDir, id, "scenarios.json"))

		var scenarios map[string]struct {
			Features map[string]struct {
				Version string `json:"version"`
			} `json:"features"`
		}
		if err := json.Unmarshal([]byte(readRepoFile(t, rel)), &scenarios); err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}

		for name, sc := range scenarios {
			pinned := strings.TrimPrefix(sc.Features[id].Version, "v")
			if pinned == "" || pinned == "latest" {
				continue
			}
			script := readRepoFile(t, filepath.ToSlash(filepath.Join(featureTestsDir, id, name+".sh")))
			if !strings.Contains(script, pinned) {
				t.Errorf("%s: scenario %q pins mdm %s but %s.sh never checks for it",
					rel, name, pinned, name)
			}
		}
	}
}

// TestFeatureScriptsAreExecutable guards a mode bit, because the symptom
// otherwise is a build failure inside a consumer's container rather than here.
func TestFeatureScriptsAreExecutable(t *testing.T) {
	root, err := findModRoot()
	if err != nil {
		t.Fatalf("could not locate module root: %v", err)
	}

	for id := range featureIDs(t) {
		paths := []string{filepath.Join(featuresDir, id, "install.sh")}

		dir := filepath.Join(root, featureTestsDir, id)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			if filepath.Ext(e.Name()) == ".sh" {
				paths = append(paths, filepath.Join(featureTestsDir, id, e.Name()))
			}
		}

		for _, rel := range paths {
			info, err := os.Stat(filepath.Join(root, rel))
			if err != nil {
				t.Errorf("stat %s: %v", rel, err)
				continue
			}
			if info.Mode().Perm()&0o111 == 0 {
				t.Errorf("%s is not executable (mode %v); run `chmod +x %s` and commit it",
					filepath.ToSlash(rel), info.Mode().Perm(), filepath.ToSlash(rel))
			}
		}
	}
}

// --- workflows and docs -----------------------------------------------------

const (
	packageJSON = "package.json"
	packageLock = "package-lock.json"
	cliPackage  = "@devcontainers/cli"
)

// yamlComment matches a whole-line # comment. Workflow prose routinely quotes
// the commands a test forbids, so a substring check over the raw file would
// fail on the comment explaining the rule.
var yamlComment = regexp.MustCompile(`(?m)^\s*#.*$`)

func stripYAMLComments(s string) string { return yamlComment.ReplaceAllString(s, "") }

// pinnedCLIVersion reads the devcontainer CLI version out of package.json,
// which is the single source of truth for it: the test workflow installs it
// with `npm ci`, and the publish workflow reads this same field at run time.
func pinnedCLIVersion(t *testing.T) string {
	t.Helper()

	var pkg struct {
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal([]byte(readRepoFile(t, packageJSON)), &pkg); err != nil {
		t.Fatalf("parse %s: %v", packageJSON, err)
	}

	v := pkg.DevDependencies[cliPackage]
	if v == "" {
		t.Fatalf("%s no longer pins %s", packageJSON, cliPackage)
	}
	if !semverPattern.MatchString(v) {
		t.Errorf("%s pins %s as %q; it must be an exact major.minor.patch version - "+
			"a range (^, ~, *) lets a CLI release change what CI runs", packageJSON, cliPackage, v)
	}
	return v
}

// TestDevcontainerCLIIsHashPinned covers the reason package.json exists at all.
// `npm install -g @devcontainers/cli@<version>` pins a version but not an
// artifact: a republish under the same version changes what CI executes, and
// OpenSSF Scorecard reads any such command as an unpinned dependency. `npm ci`
// against a lockfile carrying integrity hashes is the pinned form, and it only
// stays pinned while the lock agrees with the manifest - `npm ci` hard-fails
// otherwise, which is a red build for a reason nobody would guess from here.
func TestDevcontainerCLIIsHashPinned(t *testing.T) {
	want := pinnedCLIVersion(t)

	var lock struct {
		Packages map[string]struct {
			Version   string `json:"version"`
			Integrity string `json:"integrity"`
		} `json:"packages"`
	}
	if err := json.Unmarshal([]byte(readRepoFile(t, packageLock)), &lock); err != nil {
		t.Fatalf("parse %s: %v", packageLock, err)
	}

	entry, ok := lock.Packages["node_modules/"+cliPackage]
	switch {
	case !ok:
		t.Fatalf("%s has no entry for %s; run `npm install --package-lock-only` and commit it",
			packageLock, cliPackage)
	case entry.Version != want:
		t.Errorf("%s locks %s@%s but %s pins %s - run `npm install --package-lock-only`",
			packageLock, cliPackage, entry.Version, packageJSON, want)
	case entry.Integrity == "":
		t.Errorf("%s locks %s with no integrity hash, which is the whole point of the lock",
			packageLock, cliPackage)
	}

	// A global install would reintroduce the unpinned download the lock exists
	// to replace, and would silently win over the locked copy on PATH. Comments
	// are stripped first - the header of that workflow explains the rule by
	// quoting the command it forbids.
	testWf := stripYAMLComments(readRepoFile(t, featureTestWfPath))
	if strings.Contains(testWf, "npm install -g") {
		t.Errorf("%s installs the devcontainer CLI globally again; use `npm ci` plus "+
			"`npx devcontainer` so the run is pinned to %s", featureTestWfPath, packageLock)
	}
	if !strings.Contains(testWf, "npm ci") {
		t.Errorf("%s no longer runs `npm ci`, so nothing installs the pinned CLI", featureTestWfPath)
	}
	if !strings.Contains(testWf, "npx devcontainer features test") {
		t.Errorf("%s no longer runs `npx devcontainer features test`; a bare "+
			"`devcontainer` resolves to whatever the runner image happens to ship",
			featureTestWfPath)
	}

	// A bump to the pin has to run the tests the pin governs. Without these
	// paths the workflow never triggers on Dependabot's npm PR, and the CLI
	// version CI publishes with is one nothing ever exercised.
	for _, path := range []string{packageJSON, packageLock} {
		if !strings.Contains(testWf, `"`+path+`"`) {
			t.Errorf("%s does not list %s in its paths filter, so a bump to the "+
				"devcontainer CLI would merge without these tests running",
				featureTestWfPath, path)
		}
	}
}

// TestFeatureWorkflowsAgree keeps the workflow that tests a feature and the one
// that publishes it pointed at the same features, with the same CLI.
func TestFeatureWorkflowsAgree(t *testing.T) {
	pinned := pinnedCLIVersion(t)
	testWf := readRepoFile(t, featureTestWfPath)
	releaseWf := readRepoFile(t, featureReleaseWf)

	// The publish workflow must derive the CLI version from package.json rather
	// than repeat it, so the CLI that validated the feature on the PR is
	// necessarily the one that publishes it.
	if strings.Contains(releaseWf, "DEVCONTAINER_CLI_VERSION") {
		t.Errorf("%s pins the devcontainer CLI version itself; it must read the pin from %s "+
			"so the two workflows cannot drift apart", featureReleaseWf, packageJSON)
	}
	if !strings.Contains(releaseWf, `require('./package.json').devDependencies['`+cliPackage+`']`) {
		t.Errorf("%s no longer reads the CLI version out of %s", featureReleaseWf, packageJSON)
	}
	if !strings.Contains(releaseWf, "devcontainer-cli-version: ${{ steps.cli.outputs.version }}") {
		t.Errorf("%s does not pass the version it read to devcontainers/action", featureReleaseWf)
	}

	// The docs tell people to install that same CLI to reproduce a CI failure
	// locally, so a stale copy sends them to a version CI never ran.
	npmPin := regexp.MustCompile(regexp.QuoteMeta(cliPackage) + `@([0-9][^\s"']*)`)
	for _, rel := range []string{featureReadmePath, "AGENTS.md"} {
		for _, m := range npmPin.FindAllStringSubmatch(readRepoFile(t, rel), -1) {
			if m[1] != pinned {
				t.Errorf("%s installs %s@%s but %s pins %s", rel, cliPackage, m[1], packageJSON, pinned)
			}
		}
	}

	if !strings.Contains(releaseWf, `base-path-to-features: "./src"`) {
		t.Errorf("%s does not publish from ./src", featureReleaseWf)
	}

	// Each feature has to appear in the test matrix, or adding one quietly
	// gets it published with nothing having built it first.
	for id := range featureIDs(t) {
		if !regexp.MustCompile(`(?m)^\s*-\s*` + regexp.QuoteMeta(id) + `\s*$`).MatchString(testWf) {
			t.Errorf("%s: feature %q is missing from the test matrix", featureTestWfPath, id)
		}
	}
}

// TestFeatureREADMEDocumentsOptions keeps the options table honest. The table
// is the published documentation for the feature - consumers read it instead of
// the JSON - so a renamed option or a changed default has to land in both.
func TestFeatureREADMEDocumentsOptions(t *testing.T) {
	readme := readRepoFile(t, featureReadmePath)
	meta := featureIDs(t)["mdm"]

	for name, opt := range meta.Options {
		row := regexp.MustCompile(`(?m)^\|\s*` + regexp.QuoteMeta(name) + `\s*\|([^|]*)\|([^|]*)\|([^|]*)\|`)
		m := row.FindStringSubmatch(readme)
		if m == nil {
			t.Errorf("%s: option %q has no row in the options table", featureReadmePath, name)
			continue
		}
		if got := strings.TrimSpace(m[1]); got != opt.Description {
			t.Errorf("%s: option %q description is\n\t%s\nbut %s says\n\t%s",
				featureReadmePath, name, got, featureMetadata, opt.Description)
		}
		if got := strings.TrimSpace(m[2]); got != opt.Type {
			t.Errorf("%s: option %q type is %q, want %q", featureReadmePath, name, got, opt.Type)
		}
		if got, want := strings.TrimSpace(m[3]), fmt.Sprintf("%v", opt.Default); got != want {
			t.Errorf("%s: option %q default is %q, want %q", featureReadmePath, name, got, want)
		}
	}

	// The published image reference is the one thing every consumer copies.
	cfg := goreleaser(t)
	wantRef := fmt.Sprintf("ghcr.io/%s/%s/%s", cfg.Release.GitHub.Owner, cfg.Release.GitHub.Name, meta.ID)
	if !strings.Contains(readme, wantRef) {
		t.Errorf("%s does not show the published reference %s", featureReadmePath, wantRef)
	}
	if !strings.Contains(readRepoFile(t, "README.md"), wantRef) {
		t.Errorf("README.md does not document the dev container feature (%s)", wantRef)
	}
}
